package api

import "net/http"

type scanResponse struct {
	Controllers []scanController `json:"controllers"`
}

type scanController struct {
	HomeID       string `json:"home_id"`
	Name         string `json:"name"`
	ControllerID string `json:"controller_id"`
	API          string `json:"api"`
}

// scanControllers discovers nearby announced controllers (Homes) for the
// enrollment UI to present as a pick-list.
func (h *apiHandler) scanControllers(w http.ResponseWriter, r *http.Request) {
	found, err := h.scan(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]scanController, 0, len(found))
	for _, a := range found {
		out = append(out, scanController{
			HomeID: a.HomeID, Name: a.Name, ControllerID: a.ControllerID, API: a.API,
		})
	}
	writeJSON(w, http.StatusOK, scanResponse{Controllers: out})
}
