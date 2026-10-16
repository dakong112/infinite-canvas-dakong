package handler

import (
	"net/http"

	"github.com/tigerowo/infinite-canvas/service"
)

// BgmTracks lists the background music library of the product video wizard.
func BgmTracks(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (any, error) { return service.BgmTracks() })
}

// AdminAddBgmTrack adds an audio file the admin uploaded through /api/v1/files to the library.
func AdminAddBgmTrack(w http.ResponseWriter, r *http.Request) {
	var input service.BgmTrackInput
	if !decodeEcommerceBody(w, r, &input) {
		return
	}
	respond(w, func() (any, error) { return service.AddBgmTrack(r.Context(), input) })
}

func AdminDeleteBgmTrack(w http.ResponseWriter, r *http.Request, fileID string) {
	respond(w, func() (any, error) { return service.DeleteBgmTrack(fileID) })
}
