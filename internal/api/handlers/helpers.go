package handlers

import (
	"encoding/json"
	"io"
	"net/http"
)

// decodeJSON decodes the request body into v.
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024)).Decode(v)
}
