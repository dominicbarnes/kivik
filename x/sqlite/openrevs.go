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

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-kivik/kivik/v4/driver"
	internal "github.com/go-kivik/kivik/v4/int/errors"
)

func (d *db) OpenRevs(ctx context.Context, docID string, revs []string, options driver.Options) (driver.Rows, error) {
	opts := newOpts(options)
	values := make([]string, 0, len(revs))
	args := make([]interface{}, 5, len(revs)*2+5)
	args[0] = docID
	args[1] = len(revs) == 0 // open_revs=[]
	args[2] = false
	args[3] = opts.latest()
	args[4] = opts.revs()
	if len(revs) == 1 && revs[0] == "all" {
		args[2] = true
		revs = []string{}
	}

	i := len(args) + 1
	for _, rev := range revs {
		r, err := parseRev(rev)
		if err != nil {
			return nil, &internal.Error{Message: "invalid rev format", Status: http.StatusBadRequest}
		}
		values = append(values, fmt.Sprintf("($1, $%d, $%d)", i, i+1))
		args = append(args, r.rev, r.id)
		i += 2
	}

	if len(revs) == 0 {
		values = []string{"(NULL, NULL, NULL)"}
	}

	query := fmt.Sprintf(d.query(`
		WITH
		RECURSIVE ancestors AS (
			SELECT id, rev, rev_id, parent_rev, parent_rev_id
			FROM {{ .Revs }}
			UNION ALL
			SELECT a.id, c.rev, c.rev_id, a.parent_rev, a.parent_rev_id
			FROM {{ .Revs }} AS c
			JOIN ancestors AS a ON c.id = a.id AND c.parent_rev = a.rev AND c.parent_rev_id = a.rev_id
		),
		provided_revs (id, rev, rev_id) AS (
			VALUES %s
		),
		open_revs (id, rev, rev_id) AS (
			-- Provided revs
			SELECT *
			FROM provided_revs
			WHERE id IS NOT NULL

			UNION

			-- winning rev, for open_revs=[] case
			SELECT *
			FROM (
				SELECT
					parent.id,
					parent.rev,
					parent.rev_id
				FROM {{ .Revs }} AS parent
				LEFT JOIN {{ .Revs }} AS child ON parent.id = child.id AND parent.rev = child.parent_rev AND parent.rev_id = child.parent_rev_id
				WHERE $2 AND (parent.id = $1 AND child.id IS NULL)
				ORDER BY parent.rev DESC, parent.rev_id DESC
				LIMIT 1
			)

			UNION

			-- latest=true
			SELECT *
			FROM (
				WITH leaves AS (
					SELECT id, rev AS child_rev, rev_id AS child_rev_id, rev, rev_id, parent_rev, parent_rev_id
					FROM {{ .Revs }} AS revs
					UNION ALL
					SELECT r.id, r.rev, r.rev_id, a.rev, a.rev_id, r.parent_rev, r.parent_rev_id
					FROM {{ .Revs }} AS r
					JOIN leaves a ON r.id = a.id AND r.rev = a.parent_rev AND r.rev_id = a.parent_rev_id
				)
				SELECT
					leaves.id,
					leaves.rev AS rev,
					leaves.rev_id AS rev_id
				FROM leaves
				JOIN provided_revs AS pr ON pr.id = leaves.id AND pr.rev = leaves.child_rev AND pr.rev_id = leaves.child_rev_id
				LEFT JOIN {{ .Revs }} AS child ON leaves.id = child.id AND leaves.rev = child.parent_rev AND leaves.rev_id = child.parent_rev_id
				WHERE $4 AND child.id IS NULL
			)

			UNION

			-- all
			SELECT
				parent.id,
				parent.rev,
				parent.rev_id
			FROM {{ .Revs }} AS parent
			LEFT JOIN {{ .Revs }} AS child ON parent.id = child.id AND parent.rev = child.parent_rev AND parent.rev_id = child.parent_rev_id
			WHERE $3 AND (parent.id = $1 AND child.id IS NULL)
		)
		SELECT
			CASE WHEN row_number = 1 THEN rev END AS rev,
			CASE WHEN row_number = 1 THEN rev_id END AS rev_id,
			CASE WHEN row_number = 1 THEN deleted END AS deleted,
			CASE WHEN row_number = 1 THEN doc END AS doc,
			CASE WHEN row_number = 1 THEN
				IIF($5, COALESCE(GROUP_CONCAT(parent_rev || '-' || parent_rev_id, ","), ""), NULL)
				END AS ancestors,
			attachment_count,
			filename,
			content_type,
			length,
			digest,
			rev_pos,
			data
		FROM (
			SELECT
				open_revs.rev,
				open_revs.rev_id,
				docs.deleted,
				docs.doc,
				ancestors.parent_rev AS parent_rev,
				ancestors.parent_rev_id AS parent_rev_id,
				ROW_NUMBER() OVER (PARTITION BY open_revs.rev, open_revs.rev_id ORDER BY open_revs.rev, ancestors.rev_id, parent_rev DESC, parent_rev_id DESC) AS row_number,
				SUM(CASE WHEN ancestors.parent_rev IS NOT NULL THEN 1 ELSE 0 END) OVER (PARTITION BY open_revs.rev, open_revs.rev_id) AS parent_count,
				att.filename,
				att.content_type,
				att.length,
				att.digest,
				att.rev_pos,
				att.data,
				SUM(CASE WHEN bridge.pk IS NOT NULL THEN 1 ELSE 0 END) OVER (PARTITION BY open_revs.rev, open_revs.rev_id) AS attachment_count,
				ROW_NUMBER() OVER (PARTITION BY open_revs.rev, open_revs.rev_id) AS row_number
			FROM open_revs
			LEFT JOIN {{ .Docs }} AS docs ON open_revs.id = docs.id AND open_revs.rev = docs.rev AND open_revs.rev_id = docs.rev_id
			LEFT JOIN ancestors ON $5 AND open_revs.id = ancestors.id AND open_revs.rev = ancestors.rev AND open_revs.rev_id = ancestors.rev_id
			LEFT JOIN {{ .AttachmentsBridge }} AS bridge ON open_revs.id = bridge.id AND open_revs.rev = bridge.rev AND open_revs.rev_id = bridge.rev_id
			LEFT JOIN {{ .Attachments }} AS att ON bridge.pk = att.pk
			ORDER BY open_revs.rev, open_revs.rev_id, parent_rev DESC, parent_rev_id DESC
		)
		GROUP BY rev, rev_id, deleted, doc, attachment_count, filename, content_type, length, digest, rev_pos, data
	`), strings.Join(values, ", "))
	rows, err := d.db.QueryContext(ctx, query, args...) //nolint:rowserrcheck // Err checked in Next
	if err != nil {
		return nil, d.errDatabaseNotFound(err)
	}

	// Call rows.Next() to see if we get any results at all. If zero results,
	// we need to return 404 instead of an iterator for the open_revs=all case.
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, &internal.Error{Message: "missing", Status: http.StatusNotFound}
	}

	return &openRevsRows{
		id:   docID,
		ctx:  ctx,
		pre:  true,
		rows: rows,
	}, nil
}

type openRevsRows struct {
	id  string
	ctx context.Context
	// pre is during instantiation to indicate that the first call to Next has
	// already been done, so Next() should skip the next call to Next()
	pre  bool
	rows *sql.Rows
}

var _ driver.Rows = (*openRevsRows)(nil)

func (r *openRevsRows) Next(row *driver.Row) error {
	var (
		doc             fullDoc
		attachmentCount = 1
	)
	for {
		if !r.pre && !r.rows.Next() {
			if err := r.rows.Err(); err != nil {
				return err
			}
			return io.EOF
		}
		r.pre = false
		var (
			rowRev                *int
			rowRevID, ancestors   *string
			rowDeleted            *bool
			rowDoc                *[]byte
			filename, contentType *string
			length                *int64
			revPos                *int
			digest                *md5sum
			data                  *[]byte
		)
		if err := r.rows.Scan(
			&rowRev, &rowRevID, &rowDeleted, &rowDoc, &ancestors,
			&attachmentCount, &filename, &contentType, &length, &digest, &revPos, &data,
		); err != nil {
			return err
		}
		if rowRev != nil {
			rv := revision{rev: *rowRev, id: *rowRevID}
			row.Rev = rv.String()
			row.ID = r.id
			if rowDeleted == nil {
				row.Error = &internal.Error{Message: "missing", Status: http.StatusNotFound}
				return nil
			}

			doc = fullDoc{
				ID:      r.id,
				Rev:     rv.String(),
				Doc:     *rowDoc,
				Deleted: *rowDeleted,
			}
			if ancestors != nil {
				doc.Revisions = &revsInfo{
					Start: rv.rev,
					IDs:   []string{rv.id},
				}
				if len(*ancestors) > 0 {
					for i, ancestor := range strings.Split(*ancestors, ",") {
						a, _ := parseRev(ancestor)
						if rv.rev-1-i != a.rev {
							// missing a historical rev; this should not happen
							// but to be safe, we'll be sure not to send a history
							// with gaps
							break
						}
						doc.Revisions.IDs = append(doc.Revisions.IDs, a.id)
					}
				}
			}
		}

		if filename != nil {
			if doc.Attachments == nil {
				doc.Attachments = map[string]*attachment{}
			}
			att := &attachment{
				ContentType: *contentType,
				Digest:      *digest,
				Length:      *length,
				RevPos:      *revPos,
			}
			att.Data, _ = json.Marshal(*data)
			doc.Attachments[*filename] = att
		}

		if attachmentCount == len(doc.Attachments) {
			row.Doc = doc.toReader()
			return nil
		}
	}
}

func (r *openRevsRows) Close() error {
	return r.rows.Close()
}

func (*openRevsRows) Offset() int64     { return 0 }
func (*openRevsRows) UpdateSeq() string { return "" }
func (*openRevsRows) TotalRows() int64  { return 0 }
