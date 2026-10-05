package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) recordInspection(w http.ResponseWriter, r *http.Request) {
	var request contracts.InspectionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RecordInspection(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
