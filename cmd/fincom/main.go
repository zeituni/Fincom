package main

import (
	"alerts/handler"
	"alerts/persistence"
	"alerts/service"
	"log"
	"net/http"
)

func main() {
	// --- wire dependencies ---
	store := persistence.NewMemoryStore()
	svc := service.NewAlertService(store)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// --- wrap with tenant middleware ---
	root := tenantMiddleware(mux)

	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", root); err != nil {
		log.Fatal(err)
	}
}

// tenantMiddleware is a stub that extracts the tenant from a request header.
// Replace with real JWT/session logic; the contract is just: call WithTenantID
// and put the result back on the context before passing to the next handler.
func tenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" {
			http.Error(w, `{"error":"missing X-Tenant-ID header"}`, http.StatusForbidden)
			return
		}
		ctx := handler.WithTenantID(r.Context(), tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
