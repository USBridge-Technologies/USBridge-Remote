/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package httputils contains a bunch of HTTP utility code, some generic
package httputils

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gostor/gotgt/pkg/version"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

// APIVersionKey is the client's requested API version.
const APIVersionKey = "api-version"

// APIFunc is an adapter to allow the use of ordinary functions as Docker API endpoints.
// Any function that has the appropriate signature can be register as a API endpoint (e.g. getVersion).
type APIFunc func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error

// HijackConnection interrupts the http response writer to get the
// underlying connection and operate with it.
func HijackConnection(w http.ResponseWriter) (io.ReadCloser, io.Writer, error) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	// Flush the options to make sure the client sets the raw mode
	conn.Write([]byte{})
	return conn, conn, nil
}

// CloseStreams ensures that a list for http streams are properly closed.
func CloseStreams(streams ...interface{}) {
	for _, stream := range streams {
		if tcpc, ok := stream.(interface {
			CloseWrite() error
		}); ok {
			tcpc.CloseWrite()
		} else if closer, ok := stream.(io.Closer); ok {
			closer.Close()
		}
	}
}

// MatchesContentType validates the content type against the expected one
func MatchesContentType(contentType, expectedType string) bool {
	mimetype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		logrus.Errorf("Error parsing media type: %s error: %v", contentType, err)
	}
	return err == nil && mimetype == expectedType
}

// CheckForJSON makes sure that the request's Content-Type is application/json.
func CheckForJSON(r *http.Request) error {
	ct := r.Header.Get("Content-Type")

	// No Content-Type header is ok as long as there's no Body
	if ct == "" {
		if r.Body == nil || r.ContentLength == 0 {
			return nil
		}
	}

	// Otherwise it better be json
	if MatchesContentType(ct, "application/json") {
		return nil
	}
	return fmt.Errorf("Content-Type specified (%s) must be 'application/json'", ct)
}

// ParseForm ensures the request form is parsed even with invalid content types.
// If we don't do this, POST method without Content-type (even with empty body) will fail.
func ParseForm(r *http.Request) error {
	if r == nil {
		return nil
	}
	if err := r.ParseForm(); err != nil && !strings.HasPrefix(err.Error(), "mime:") {
		return err
	}
	return nil
}

// ParseMultipartForm ensure the request form is parsed, even with invalid content types.
func ParseMultipartForm(r *http.Request) error {
	if err := r.ParseMultipartForm(4096); err != nil && !strings.HasPrefix(err.Error(), "mime:") {
		return err
	}
	return nil
}

// WriteError decodes a specific docker error and sends it in the response.
func WriteError(w http.ResponseWriter, err error) {
	if err == nil || w == nil {
		logrus.WithFields(logrus.Fields{"error": err, "writer": w}).Error("unexpected HTTP error handling")
		return
	}

	statusCode := http.StatusInternalServerError
	errMsg := err.Error()

	// This part of will be removed once we've
	// converted everything over to use the errcode package

	// FIXME: this is brittle and should not be necessary.
	// If we need to differentiate between different possible error types,
	// we should create appropriate error types with clearly defined meaning
	errStr := strings.ToLower(err.Error())
	for keyword, status := range map[string]int{
		"not found":             http.StatusNotFound,
		"no such":               http.StatusNotFound,
		"bad parameter":         http.StatusBadRequest,
		"conflict":              http.StatusConflict,
		"impossible":            http.StatusNotAcceptable,
		"wrong login/password":  http.StatusUnauthorized,
		"hasn't been activated": http.StatusForbidden,
	} {
		if strings.Contains(errStr, keyword) {
			statusCode = status
			break
		}
	}

	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	http.Error(w, errMsg, statusCode)
}

// WriteJSON writes the value v to the http response stream as json with standard json encoding.
func WriteJSON(w http.ResponseWriter, code int, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(v)
}

// VersionFromContext returns an API version from the context using APIVersionKey.
// It panics if the context value does not have version.Version type.
func VersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return version.Version
	}
	val := ctx.Value(APIVersionKey)
	if val == nil {
		return version.Version
	}
	return val.(string)
}
