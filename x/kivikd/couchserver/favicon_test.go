// Licensed under the Apache License, Version 2.0 (the "License"); you may not
// use this file except in compliance with the License. You may obtain a copy of
// the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations under
// the License.

//go:build !js

package couchserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFavicoDefault(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	handler := h.GetFavicon()
	handler(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	embedded, err := files.Open("files/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := io.ReadAll(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Unexpected file contents. %d bytes instead of %d", buf.Len(), len(expected))
	}
}

func TestFavicoNotFound(t *testing.T) {
	h := &Handler{Favicon: "some/invalid/path"}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	handler := h.GetFavicon()
	handler(w, req)
	resp := w.Result()
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status: %d, actual: %d", http.StatusNotFound, resp.StatusCode)
	}
}

func TestFavicoExternalFile(t *testing.T) {
	h := &Handler{Favicon: "./files/favicon.ico"}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	handler := h.GetFavicon()
	handler(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	embedded, err := files.Open("files/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := io.ReadAll(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("Unexpected file contents. %d bytes instead of %d", buf.Len(), len(expected))
	}
}
