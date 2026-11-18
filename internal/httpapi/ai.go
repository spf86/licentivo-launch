package httpapi

import (
	"io"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/app"
	"net/http"
)

func (a *application) registerAI(mux *http.ServeMux) {
	// The guard runs before parsing any payload; customer, operations and support
	// accounts cannot use even discovery, history, confirmation or downloads.
	register := func(pattern string, limit int, fn endpoint) {
		a.route(mux, pattern, true, limit, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
			if !p.IsAdmin() {
				return nil, app.ErrForbidden
			}
			return fn(w, r, p)
		})
	}
	register("GET /api/v1/admin/ai/providers", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIProviders(r.Context(), p)
	})
	register("POST /api/v1/admin/ai/providers/{id}/refresh", 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.RefreshAIModels(r.Context(), p, r.PathValue("id"))
	})
	register("POST /api/v1/admin/ai/uploads", 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		r.Body = http.MaxBytesReader(w, r.Body, (9 << 20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return nil, app.ErrInvalid
		}
		defer r.MultipartForm.RemoveAll()
		files := r.MultipartForm.File["file"]
		if len(files) != 1 || len(r.MultipartForm.File) != 1 {
			return nil, app.ErrInvalid
		}
		file, err := files[0].Open()
		if err != nil {
			return nil, app.ErrInvalid
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, ai.MaxUpload+1))
		if err != nil {
			return nil, app.ErrInvalid
		}
		return a.service.SaveAIUpload(r.Context(), p, files[0].Filename, data)
	})
	register("GET /api/v1/admin/ai/uploads/{id}", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIUpload(r.Context(), p, r.PathValue("id"))
	})
	register("POST /api/v1/admin/ai/uploads/{id}/discard", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return nil, a.service.DiscardAIUpload(r.Context(), p, r.PathValue("id"))
	})
	register("PUT /api/v1/admin/ai/providers", 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIProviderInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveAIProvider(r.Context(), p, in)
	})
	register("GET /api/v1/admin/ai/conversations", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIConversations(r.Context(), p)
	})
	register("POST /api/v1/admin/ai/conversations", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			ProviderID string `json:"provider_id"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CreateAIConversation(r.Context(), p, in.ProviderID)
	})
	register("GET /api/v1/admin/ai/conversations/{id}", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIConversation(r.Context(), p, r.PathValue("id"))
	})
	register("POST /api/v1/admin/ai/conversations/{id}/command", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIConversationCommand
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return nil, a.service.ChangeAIConversation(r.Context(), p, r.PathValue("id"), in)
	})
	register("POST /api/v1/admin/ai/conversations/{id}/steer", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AISteerInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SteerAIConversation(r.Context(), p, r.PathValue("id"), in)
	})
	register("POST /api/v1/admin/ai/conversations/{id}/messages", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIChatInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SendAIMessage(r.Context(), p, r.PathValue("id"), in)
	})
	register("GET /api/v1/admin/ai/tasks", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		limit, offset, err := listPagination(r)
		if err != nil {
			return nil, err
		}
		return a.service.AITasks(r.Context(), p, r.URL.Query().Get("status"), limit, offset, r.URL.Query().Get("q"))
	})
	register("POST /api/v1/admin/ai/tasks/{id}/decision", 30, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Decision string `json:"decision"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.DecideAITask(r.Context(), p, r.PathValue("id"), in.Decision)
	})
	register("GET /api/v1/admin/ai/tasks/{id}", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AITask(r.Context(), p, r.PathValue("id"))
	})
	register("POST /api/v1/admin/ai/media", 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIMediaInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CreateAIMedia(r.Context(), p, in)
	})
	register("GET /api/v1/admin/ai/artifacts/{id}", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIArtifact(r.Context(), p, r.PathValue("id"))
	})
	register("GET /api/v1/admin/ai/schedules", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AISchedules(r.Context(), p)
	})
	register("GET /api/v1/admin/ai/operations", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		limit, offset, err := listPagination(r)
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return a.service.AIOperations(r.Context(), p, app.AIOperationQuery{Query: q.Get("q"), Kind: q.Get("kind"), Status: q.Get("status"), From: q.Get("from"), To: q.Get("to"), Limit: limit, Offset: offset})
	})
	register("PUT /api/v1/admin/ai/schedules", 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIScheduleInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveAISchedule(r.Context(), p, in)
	})
	register("GET /api/v1/admin/ai/mcp-connections", 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AIMCPConnections(r.Context(), p)
	})
	register("POST /api/v1/admin/ai/mcp-connections", 10, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.AIMCPInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CreateAIMCPConnection(r.Context(), p, in)
	})
	register("POST /api/v1/admin/ai/mcp-connections/{id}/revoke", 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return nil, a.service.RevokeAIMCPConnection(r.Context(), p, r.PathValue("id"))
	})
	a.registerAIMCP(mux)
}
