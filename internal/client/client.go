// Package client implements the node side of the enrollment protocol: it
// discovers a controller, proves control of its key, waits for approval, and
// applies the returned profile. A stateless state machine tracks the node
// lifecycle described in the README.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/qmuntal/stateless"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
)

// State is a node lifecycle state.
type State string

const (
	StateUnclaimed       State = "unclaimed"
	StateEnrolling       State = "enrolling"
	StatePendingApproval State = "pending_approval"
	StateActive          State = "active"
	StateFailed          State = "failed"
)

// triggers
const (
	trigEnroll  = "enroll"
	trigPending = "pending"
	trigApprove = "approve"
	trigFail    = "fail"
)

// Options configures a Client.
type Options struct {
	HTTPClient   *http.Client
	PollInterval time.Duration
	PollTimeout  time.Duration
}

// Client enrolls a single node into a controller.
type Client struct {
	id      *identity.Identity
	base    string
	http    *http.Client
	poll    time.Duration
	machine *stateless.StateMachine
}

// New creates a client for the given identity targeting controllerURL.
func New(id *identity.Identity, controllerURL string, opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	poll := opts.PollInterval
	if poll == 0 {
		poll = 2 * time.Second
	}

	m := stateless.NewStateMachine(StateUnclaimed)
	m.Configure(StateUnclaimed).Permit(trigEnroll, StateEnrolling).Permit(trigFail, StateFailed)
	m.Configure(StateEnrolling).
		Permit(trigPending, StatePendingApproval).
		Permit(trigApprove, StateActive).
		Permit(trigFail, StateFailed)
	m.Configure(StatePendingApproval).Permit(trigApprove, StateActive).Permit(trigFail, StateFailed)
	m.Configure(StateActive)
	m.Configure(StateFailed)

	return &Client{
		id:      id,
		base:    strings.TrimRight(controllerURL, "/"),
		http:    httpClient,
		poll:    poll,
		machine: m,
	}
}

// State returns the current lifecycle state.
func (c *Client) State() State {
	return c.machine.MustState().(State)
}

// Enroll runs the full enrollment exchange and returns the final result. On any
// error the state machine transitions to Failed.
func (c *Client) Enroll(ctx context.Context, serial string) (enrollment.Result, error) {
	res, err := c.enroll(ctx, serial)
	if err != nil {
		_ = c.machine.Fire(trigFail)
		return enrollment.Result{}, err
	}
	return res, nil
}

func (c *Client) enroll(ctx context.Context, serial string) (enrollment.Result, error) {
	if err := c.machine.Fire(trigEnroll); err != nil {
		return enrollment.Result{}, err
	}

	// 1. request a challenge
	var reqRes enrollment.RequestResult
	if err := c.post(ctx, "/enroll/request", enrollment.RequestInput{
		NodeID:    c.id.NodeID(),
		Serial:    serial,
		PublicKey: c.id.PublicKeyDER(),
	}, &reqRes); err != nil {
		return enrollment.Result{}, fmt.Errorf("enroll request: %w", err)
	}

	// 2. sign the challenge and verify
	sig, err := c.id.Sign(reqRes.Challenge)
	if err != nil {
		return enrollment.Result{}, err
	}
	var result enrollment.Result
	if err := c.post(ctx, "/enroll/verify", enrollment.VerifyInput{
		EnrollmentID: reqRes.EnrollmentID,
		Signature:    sig,
	}, &result); err != nil {
		return enrollment.Result{}, fmt.Errorf("enroll verify: %w", err)
	}

	// 3. wait for approval if the controller requires manual adoption
	if result.Status == models.EnrollmentPendingApproval {
		if err := c.machine.Fire(trigPending); err != nil {
			return enrollment.Result{}, err
		}
		result, err = c.waitForApproval(ctx, reqRes.EnrollmentID)
		if err != nil {
			return enrollment.Result{}, err
		}
	}

	if result.Status != models.EnrollmentApproved && result.Status != models.EnrollmentActive {
		return enrollment.Result{}, fmt.Errorf("unexpected enrollment status %q", result.Status)
	}

	// 4. acknowledge to become active
	var acked enrollment.Result
	if err := c.post(ctx, "/enroll/"+reqRes.EnrollmentID+"/ack", nil, &acked); err != nil {
		return enrollment.Result{}, fmt.Errorf("enroll ack: %w", err)
	}
	if err := c.machine.Fire(trigApprove); err != nil {
		return enrollment.Result{}, err
	}
	return acked, nil
}

func (c *Client) waitForApproval(ctx context.Context, enrollmentID string) (enrollment.Result, error) {
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	for {
		var result enrollment.Result
		if err := c.get(ctx, "/enroll/"+enrollmentID, &result); err != nil {
			return enrollment.Result{}, err
		}
		switch result.Status {
		case models.EnrollmentApproved, models.EnrollmentActive:
			return result, nil
		case models.EnrollmentRejected:
			return enrollment.Result{}, fmt.Errorf("enrollment rejected")
		}

		select {
		case <-ctx.Done():
			return enrollment.Result{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			return err
		}
		body = buf
	}
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
