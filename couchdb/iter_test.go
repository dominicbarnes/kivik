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

package couchdb

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gitlab.com/flimzy/testy"
)

func TestCancelableReadCloser(t *testing.T) {
	t.Run("no cancellation", func(t *testing.T) {
		t.Parallel()
		rc := newCancelableReadCloser(
			context.Background(),
			io.NopCloser(strings.NewReader("foo")),
		)
		result, err := io.ReadAll(rc)
		if !testy.ErrorMatches("", err) {
			t.Errorf("Unexpected error: %s", err)
		}
		if string(result) != "foo" {
			t.Errorf("Unexpected result: %s", string(result))
		}
	})
	t.Run("pre-cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rc := newCancelableReadCloser(
			ctx,
			io.NopCloser(io.MultiReader(
				testy.DelayReader(100*time.Millisecond),
				strings.NewReader("foo")),
			),
		)
		result, err := io.ReadAll(rc)
		if !testy.ErrorMatches("context canceled", err) {
			t.Errorf("Unexpected error: %s", err)
		}
		if string(result) != "" {
			t.Errorf("Unexpected result: %s", string(result))
		}
	})
	t.Run("canceled mid-flight", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		r := io.MultiReader(
			strings.NewReader("foo"),
			testy.DelayReader(time.Second),
			strings.NewReader("bar"),
		)
		rc := newCancelableReadCloser(
			ctx,
			io.NopCloser(r),
		)
		_, err := io.ReadAll(rc)
		if !testy.ErrorMatches("context deadline exceeded", err) {
			t.Errorf("Unexpected error: %s", err)
		}
	})
	t.Run("read error, not canceled", func(t *testing.T) {
		t.Parallel()
		rc := newCancelableReadCloser(
			context.Background(),
			io.NopCloser(testy.ErrorReader("foo", errors.New("read err"))),
		)
		_, err := io.ReadAll(rc)
		if !testy.ErrorMatches("read err", err) {
			t.Errorf("Unexpected error: %s", err)
		}
	})
	t.Run("closed early", func(t *testing.T) {
		t.Parallel()
		rc := newCancelableReadCloser(
			context.Background(),
			io.NopCloser(testy.NeverReader()),
		)
		_ = rc.Close()
		result, err := io.ReadAll(rc)
		if !testy.ErrorMatches("iterator closed", err) {
			t.Errorf("Unexpected error: %s", err)
		}
		if string(result) != "" {
			t.Errorf("Unexpected result: %s", string(result))
		}
	})
}
