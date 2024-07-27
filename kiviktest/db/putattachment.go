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

package db

import (
	"context"
	"io"
	"strings"

	"github.com/go-kivik/kivik/v4"
	"github.com/go-kivik/kivik/v4/kiviktest/kt"
)

func init() {
	kt.Register("PutAttachment", putAttachment)
}

func putAttachment(ctx *kt.Context) {
	ctx.RunRW(func(ctx *kt.Context) {
		dbname := ctx.TestDB()
		ctx.Run("group", func(ctx *kt.Context) {
			ctx.RunAdmin(func(ctx *kt.Context) {
				ctx.Parallel()
				testPutAttachment(ctx, ctx.Admin, dbname)
			})
			ctx.RunNoAuth(func(ctx *kt.Context) {
				ctx.Parallel()
				testPutAttachment(ctx, ctx.NoAuth, dbname)
			})
		})
	})
}

func testPutAttachment(ctx *kt.Context, client *kivik.Client, dbname string) {
	db := client.DB(dbname, ctx.Options("db"))
	if err := db.Err(); err != nil {
		ctx.Fatalf("Failed to open db: %s", err)
	}
	adb := ctx.Admin.DB(dbname, ctx.Options("db"))
	if err := adb.Err(); err != nil {
		ctx.Fatalf("Failed to open admin db: %s", err)
	}
	ctx.Run("Update", func(ctx *kt.Context) {
		ctx.Parallel()
		var docID, rev string
		err := kt.Retry(func() error {
			var e error
			docID, rev, e = adb.CreateDoc(context.Background(), map[string]string{"name": "Robert"})
			return e
		})
		if err != nil {
			ctx.Fatalf("Failed to create doc: %s", err)
		}
		err = kt.Retry(func() error {
			att := &kivik.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				Content:     stringReadCloser(),
			}
			_, err = db.PutAttachment(context.Background(), docID, att, kivik.Rev(rev))
			return err
		})
		ctx.CheckError(err)
	})
	ctx.Run("Create", func(ctx *kt.Context) {
		ctx.Parallel()
		docID := ctx.TestDBName()
		err := kt.Retry(func() error {
			att := &kivik.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				Content:     stringReadCloser(),
			}
			_, err := db.PutAttachment(context.Background(), docID, att)
			return err
		})
		ctx.CheckError(err)
	})
	ctx.Run("Conflict", func(ctx *kt.Context) {
		ctx.Parallel()
		var docID string
		err2 := kt.Retry(func() error {
			var e error
			docID, _, e = adb.CreateDoc(context.Background(), map[string]string{"name": "Robert"})
			return e
		})
		if err2 != nil {
			ctx.Fatalf("Failed to create doc: %s", err2)
		}
		err := kt.Retry(func() error {
			att := &kivik.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				Content:     stringReadCloser(),
			}
			_, err := db.PutAttachment(context.Background(), docID, att, kivik.Rev("5-20bd3c7d7d6b81390c6679d8bae8795b"))
			return err
		})
		ctx.CheckError(err)
	})
	ctx.Run("UpdateDesignDoc", func(ctx *kt.Context) {
		ctx.Parallel()
		docID := "_design/" + ctx.TestDBName()
		doc := map[string]string{
			"_id": docID,
		}
		var rev string
		err := kt.Retry(func() error {
			var err error
			rev, err = adb.Put(context.Background(), docID, doc)
			return err
		})
		if err != nil {
			ctx.Fatalf("Failed to create design doc: %s", err)
		}
		err = kt.Retry(func() error {
			att := &kivik.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				Content:     stringReadCloser(),
			}
			_, err = db.PutAttachment(context.Background(), docID, att, kivik.Rev(rev))
			return err
		})
		ctx.CheckError(err)
	})
	ctx.Run("CreateDesignDoc", func(ctx *kt.Context) {
		ctx.Parallel()
		docID := "_design/" + ctx.TestDBName()
		err := kt.Retry(func() error {
			att := &kivik.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				Content:     stringReadCloser(),
			}
			_, err := db.PutAttachment(context.Background(), docID, att)
			return err
		})
		ctx.CheckError(err)
	})
}

func stringReadCloser() io.ReadCloser {
	return io.NopCloser(strings.NewReader("test content"))
}
