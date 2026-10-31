package httpapi

import (
	"licentivo-launch/internal/app"
	"net/http"
	"strconv"
)

func listPagination(r *http.Request) (int, int, error) {
	limit, offset := 25, 0
	for name, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if r.URL.Query().Has(name) {
			value, err := strconv.Atoi(r.URL.Query().Get(name))
			if err != nil || (name == "limit" && value == 0) {
				return 0, 0, app.ErrInvalid
			}
			*target = value
		}
	}
	return limit, offset, nil
}
