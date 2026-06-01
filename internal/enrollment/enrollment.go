// Package enrollment implements the controller side of the node enrollment
// protocol: it issues challenges, verifies that a node controls its key,
// approves (adopts) nodes, and records them in the inventory.
package enrollment

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

const challengeBytes = 32

var (
	// ErrIdentityMismatch is returned when a node_id does not match the hash of
	// the submitted public key.
	ErrIdentityMismatch = errors.New("enrollment: node_id does not match public key")
	// ErrInvalidSignature is returned when the challenge signature does not
	// verify against the enrolled public key.
	ErrInvalidSignature = errors.New("enrollment: invalid challenge signature")
	// ErrNotAdoptable is returned when adopting an enrollment that is not
	// awaiting approval.
	ErrNotAdoptable = errors.New("enrollment: not awaiting approval")
)

// Repository is the persistence surface the service depends on. The storage
// layer's Store satisfies it.
type Repository interface {
	CreateEnrollment(ctx context.Context, e models.Enrollment) error
	GetEnrollment(ctx context.Context, id string) (models.Enrollment, error)
	GetEnrollmentByNodeID(ctx context.Context, nodeID string) (models.Enrollment, error)
	UpdateEnrollment(ctx context.Context, e models.Enrollment) error
	CreateNode(ctx context.Context, node models.Node) error
	GetNode(ctx context.Context, id string) (models.Node, error)
	GetProfile(ctx context.Context, homeID string) (models.Profile, error)
}

// Options configures a Service. Zero values fall back to sensible defaults.
type Options struct {
	HomeID    string
	AutoAdopt bool
	Rand      io.Reader     // challenge entropy; defaults to crypto/rand
	Now       func() int64  // unix-seconds clock; defaults to time.Now
	NewID     func() string // enrollment id generator; defaults to UUID
}

// Service runs the controller-side enrollment logic.
type Service struct {
	repo Repository
	opts Options
}

// NewService builds a Service, filling in default option values.
func NewService(repo Repository, opts Options) *Service {
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().Unix() }
	}
	if opts.NewID == nil {
		opts.NewID = uuid.NewString
	}
	return &Service{repo: repo, opts: opts}
}

// RequestInput is the body of POST /enroll/request.
type RequestInput struct {
	NodeID    string `json:"node_id"`
	Serial    string `json:"serial"`
	PublicKey []byte `json:"public_key"`
}

// RequestResult is returned from POST /enroll/request.
type RequestResult struct {
	EnrollmentID string `json:"enrollment_id"`
	Challenge    []byte `json:"challenge"`
}

// VerifyInput is the body of POST /enroll/verify.
type VerifyInput struct {
	EnrollmentID string `json:"enrollment_id"`
	Signature    []byte `json:"signature"`
}

// Result is the status (and profile, once approved) of an enrollment.
type Result struct {
	Status  models.EnrollmentStatus `json:"status"`
	Profile *models.Profile         `json:"profile,omitempty"`
}

// Request records an enrollment request and returns a fresh challenge. A node
// re-requesting (same node_id) re-issues the challenge against its record.
func (s *Service) Request(ctx context.Context, in RequestInput) (RequestResult, error) {
	if in.NodeID == "" || len(in.PublicKey) == 0 {
		return RequestResult{}, errors.New("enrollment: node_id and public_key are required")
	}
	if identity.NodeIDFromPublicKeyDER(in.PublicKey) != in.NodeID {
		return RequestResult{}, ErrIdentityMismatch
	}

	challenge := make([]byte, challengeBytes)
	if _, err := io.ReadFull(s.opts.Rand, challenge); err != nil {
		return RequestResult{}, fmt.Errorf("generate challenge: %w", err)
	}

	existing, err := s.repo.GetEnrollmentByNodeID(ctx, in.NodeID)
	switch {
	case err == nil:
		existing.Serial = in.Serial
		existing.PublicKey = in.PublicKey
		existing.Challenge = challenge
		existing.Status = models.EnrollmentPendingVerification
		if err := s.repo.UpdateEnrollment(ctx, existing); err != nil {
			return RequestResult{}, err
		}
		return RequestResult{EnrollmentID: existing.ID, Challenge: challenge}, nil
	case isNotFound(err):
		e := models.Enrollment{
			ID:        s.opts.NewID(),
			NodeID:    in.NodeID,
			Serial:    in.Serial,
			PublicKey: in.PublicKey,
			Challenge: challenge,
			Status:    models.EnrollmentPendingVerification,
			HomeID:    s.opts.HomeID,
			CreatedAt: s.opts.Now(),
		}
		if err := s.repo.CreateEnrollment(ctx, e); err != nil {
			return RequestResult{}, err
		}
		return RequestResult{EnrollmentID: e.ID, Challenge: challenge}, nil
	default:
		return RequestResult{}, err
	}
}

// Verify checks the challenge signature. On success the enrollment advances to
// pending_approval, and is adopted immediately when AutoAdopt is set.
func (s *Service) Verify(ctx context.Context, in VerifyInput) (Result, error) {
	e, err := s.repo.GetEnrollment(ctx, in.EnrollmentID)
	if err != nil {
		return Result{}, err
	}

	ok, err := identity.VerifySignature(e.PublicKey, e.Challenge, in.Signature)
	if err != nil || !ok {
		return Result{}, ErrInvalidSignature
	}

	e.Status = models.EnrollmentPendingApproval
	if err := s.repo.UpdateEnrollment(ctx, e); err != nil {
		return Result{}, err
	}

	if s.opts.AutoAdopt {
		return s.approve(ctx, e)
	}
	return s.result(ctx, e), nil
}

// Adopt approves a node that has passed verification and is awaiting approval.
func (s *Service) Adopt(ctx context.Context, nodeID string) (Result, error) {
	e, err := s.repo.GetEnrollmentByNodeID(ctx, nodeID)
	if err != nil {
		return Result{}, err
	}
	if e.Status != models.EnrollmentPendingApproval {
		return Result{}, ErrNotAdoptable
	}
	return s.approve(ctx, e)
}

// Get returns the current status (and profile, if available) of an enrollment.
func (s *Service) Get(ctx context.Context, id string) (Result, error) {
	e, err := s.repo.GetEnrollment(ctx, id)
	if err != nil {
		return Result{}, err
	}
	return s.result(ctx, e), nil
}

// Ack marks an approved enrollment active once the node has applied its config.
func (s *Service) Ack(ctx context.Context, id string) (Result, error) {
	e, err := s.repo.GetEnrollment(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if e.Status == models.EnrollmentApproved {
		e.Status = models.EnrollmentActive
		if err := s.repo.UpdateEnrollment(ctx, e); err != nil {
			return Result{}, err
		}
	}
	return s.result(ctx, e), nil
}

// approve creates the node inventory record and marks the enrollment approved.
func (s *Service) approve(ctx context.Context, e models.Enrollment) (Result, error) {
	if _, err := s.repo.GetNode(ctx, e.NodeID); isNotFound(err) {
		node := models.Node{
			ID:           e.NodeID,
			Serial:       e.Serial,
			CurrentHome:  e.HomeID,
			TrustedHomes: []string{e.HomeID},
			LastSeen:     s.opts.Now(),
		}
		if err := s.repo.CreateNode(ctx, node); err != nil {
			return Result{}, err
		}
	} else if err != nil {
		return Result{}, err
	}

	e.Status = models.EnrollmentApproved
	if err := s.repo.UpdateEnrollment(ctx, e); err != nil {
		return Result{}, err
	}
	return s.result(ctx, e), nil
}

// result builds the API result for an enrollment, attaching the Home's profile
// once the node is approved or active.
func (s *Service) result(ctx context.Context, e models.Enrollment) Result {
	res := Result{Status: e.Status}
	if e.Status == models.EnrollmentApproved || e.Status == models.EnrollmentActive {
		if profile, err := s.repo.GetProfile(ctx, e.HomeID); err == nil {
			res.Profile = &profile
		}
	}
	return res
}

func isNotFound(err error) bool {
	return errors.Is(err, storage.ErrNotFound)
}
