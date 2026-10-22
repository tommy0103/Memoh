package handlers

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/labstack/echo/v4"
)

// Management requests have no device selector. Strict decoding keeps stale
// clients from silently executing their former remote request in a container.
func bindWorkspaceManagementRequest(c echo.Context, value any) error {
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON request")
	}
	return nil
}
