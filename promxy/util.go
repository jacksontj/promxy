package promxy

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

func CORSWrap(h httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// COORS headers required
		w.Header().Set("Access-Control-Allow-Origin", "*")
		h(w, r, ps)
	}
}
