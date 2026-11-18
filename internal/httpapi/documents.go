package httpapi

import (
	"licentivo-launch/internal/app"
	"net/http"
)

func (a *application) registerDocuments(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/admin/documents/{product}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.DocumentLibrary(r.Context(), p, r.PathValue("product"))
	})
	a.route(mux, "POST /api/v1/admin/documents/{product}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.DocumentCommand
		if err := inputLimit(w, r, &in, 512<<10); err != nil {
			return nil, err
		}
		return a.service.ChangeDocumentLibrary(r.Context(), p, r.PathValue("product"), in)
	})
}
