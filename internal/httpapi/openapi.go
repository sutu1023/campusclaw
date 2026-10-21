package httpapi

import (
	"net/http"

	"campusclaw/docs"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// mountDocs serves the generated OpenAPI document and the Swagger UI. The
// document is generated from handler annotations by `make docs`; mounting it
// before the business routes keeps `/openapi.json` and `/swagger/` from being
// shadowed.
func mountDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(docs.SwaggerInfo.ReadDoc()))
	})
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/openapi.json")))
}
