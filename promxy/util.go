package promxy

import (
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/julienschmidt/httprouter"
)

type apiFunc func(r *http.Request, ps httprouter.Params) (interface{}, *promhttputil.ApiError)

func apiWrap(f apiFunc) httprouter.Handle {
	hf := httprouter.Handle(func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		promhttputil.SetCORS(w)
		if data, err := f(r, ps); err != nil {
			promhttputil.RespondError(w, err, data)
		} else if data != nil {
			promhttputil.Respond(w, data)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	// TODO: wrap in metrics
	return hf
}
