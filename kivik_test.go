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

package kivik

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"gitlab.com/flimzy/testy"

	"github.com/go-kivik/kivik/v4/driver"
	internal "github.com/go-kivik/kivik/v4/int/errors"
	"github.com/go-kivik/kivik/v4/int/mock"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		driver     driver.Driver
		driverName string
		dsn        string
		expected   *Client
		status     int
		err        string
	}{
		{
			name:       "Unregistered driver",
			driverName: "unregistered",
			dsn:        "unf",
			status:     http.StatusBadRequest,
			err:        `kivik: unknown driver "unregistered" (forgotten import?)`,
		},
		{
			name: "connection error",
			driver: &mock.Driver{
				NewClientFunc: func(_ string, _ driver.Options) (driver.Client, error) {
					return nil, errors.New("connection error")
				},
			},
			driverName: "foo",
			status:     http.StatusInternalServerError,
			err:        "connection error",
		},
		{
			name: "success",
			driver: &mock.Driver{
				NewClientFunc: func(dsn string, _ driver.Options) (driver.Client, error) {
					if dsn != "oink" {
						return nil, fmt.Errorf("Unexpected DSN: %s", dsn)
					}
					return &mock.Client{ID: "foo"}, nil
				},
			},
			driverName: "bar",
			dsn:        "oink",
			expected: &Client{
				dsn:          "oink",
				driverName:   "bar",
				driverClient: &mock.Client{ID: "foo"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.driver != nil {
				Register(test.driverName, test.driver)
			}
			result, err := New(test.driverName, test.dsn)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if d := testy.DiffInterface(test.expected, result); d != nil {
				t.Error(d)
			}
		})
	}
}

func TestClientGetters(t *testing.T) {
	driverName := "foo"
	dsn := "bar"
	c := &Client{
		driverName: driverName,
		dsn:        dsn,
	}

	t.Run("Driver", func(t *testing.T) {
		result := c.Driver()
		if result != driverName {
			t.Errorf("Unexpected result: %s", result)
		}
	})

	t.Run("DSN", func(t *testing.T) {
		result := c.DSN()
		if result != dsn {
			t.Errorf("Unexpected result: %s", result)
		}
	})
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		expected *ServerVersion
		status   int
		err      string
	}{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					VersionFunc: func(context.Context) (*driver.Version, error) {
						return nil, errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		{
			name: "success",
			client: &Client{
				driverClient: &mock.Client{
					VersionFunc: func(context.Context) (*driver.Version, error) {
						return &driver.Version{Version: "foo"}, nil
					},
				},
			},
			expected: &ServerVersion{Version: "foo"},
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.client.Version(context.Background())
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if d := testy.DiffInterface(test.expected, result); d != nil {
				t.Error(d)
			}
		})
	}
}

func TestDB(t *testing.T) {
	type Test struct {
		name     string
		client   *Client
		dbName   string
		options  Option
		expected *DB
		status   int
		err      string
	}
	tests := []Test{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					DBFunc: func(string, driver.Options) (driver.DB, error) {
						return nil, errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		func() Test {
			client := &Client{
				driverClient: &mock.Client{
					DBFunc: func(dbName string, opts driver.Options) (driver.DB, error) {
						expectedDBName := "foo"
						gotOpts := map[string]interface{}{}
						opts.Apply(gotOpts)
						wantOpts := map[string]interface{}{"foo": 123}
						if dbName != expectedDBName {
							return nil, fmt.Errorf("Unexpected dbname: %s", dbName)
						}
						if d := testy.DiffInterface(wantOpts, gotOpts); d != nil {
							return nil, fmt.Errorf("Unexpected options:\n%s", d)
						}
						return &mock.DB{ID: "abc"}, nil
					},
				},
			}
			return Test{
				name:    "success",
				client:  client,
				dbName:  "foo",
				options: Param("foo", 123),
				expected: &DB{
					client:   client,
					name:     "foo",
					driverDB: &mock.DB{ID: "abc"},
				},
			}
		}(),
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.client.DB(test.dbName, test.options)
			err := result.Err()
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if err != nil {
				return
			}
			if d := testy.DiffInterface(test.expected, result); d != nil {
				t.Error(d)
			}
		})
	}
}

func TestAllDBs(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		options  Option
		expected []string
		status   int
		err      string
	}{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					AllDBsFunc: func(context.Context, driver.Options) ([]string, error) {
						return nil, errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		{
			name: "success",
			client: &Client{
				driverClient: &mock.Client{
					AllDBsFunc: func(_ context.Context, options driver.Options) ([]string, error) {
						gotOptions := map[string]interface{}{}
						options.Apply(gotOptions)
						expectedOptions := map[string]interface{}{"foo": 123}
						if d := testy.DiffInterface(expectedOptions, gotOptions); d != nil {
							return nil, fmt.Errorf("Unexpected options:\n%s", d)
						}
						return []string{"a", "b", "c"}, nil
					},
				},
			},
			options:  Param("foo", 123),
			expected: []string{"a", "b", "c"},
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.client.AllDBs(context.Background(), test.options)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if d := testy.DiffInterface(test.expected, result); d != nil {
				t.Error(d)
			}
		})
	}
}

func TestDBExists(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		dbName   string
		options  Option
		expected bool
		status   int
		err      string
	}{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					DBExistsFunc: func(context.Context, string, driver.Options) (bool, error) {
						return false, errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		{
			name: "success",
			client: &Client{
				driverClient: &mock.Client{
					DBExistsFunc: func(_ context.Context, dbName string, opts driver.Options) (bool, error) {
						expectedDBName := "foo"
						gotOpts := map[string]interface{}{}
						opts.Apply(gotOpts)
						expectedOpts := map[string]interface{}{"foo": 123}
						if dbName != expectedDBName {
							return false, fmt.Errorf("Unexpected db name: %s", dbName)
						}
						if d := testy.DiffInterface(expectedOpts, gotOpts); d != nil {
							return false, fmt.Errorf("Unexpected opts:\n%s", d)
						}
						return true, nil
					},
				},
			},
			dbName:   "foo",
			options:  Param("foo", 123),
			expected: true,
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.client.DBExists(context.Background(), test.dbName, test.options)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if test.expected != result {
				t.Errorf("Unexpected result: %v", result)
			}
		})
	}
}

func TestCreateDB(t *testing.T) {
	tests := []struct {
		name    string
		client  *Client
		dbName  string
		options Option
		status  int
		err     string
	}{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					CreateDBFunc: func(context.Context, string, driver.Options) error {
						return errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		{
			name: "success",
			client: &Client{
				driverClient: &mock.Client{
					CreateDBFunc: func(_ context.Context, dbName string, options driver.Options) error {
						expectedDBName := "foo"
						wantOpts := map[string]interface{}{"foo": 123}
						gotOpts := map[string]interface{}{}
						options.Apply(gotOpts)
						if dbName != expectedDBName {
							return fmt.Errorf("Unexpected dbname: %s", dbName)
						}
						if d := testy.DiffInterface(wantOpts, gotOpts); d != nil {
							return fmt.Errorf("Unexpected opts:\n%s", d)
						}
						return nil
					},
					DBFunc: func(string, driver.Options) (driver.DB, error) {
						return &mock.DB{ID: "abc"}, nil
					},
				},
			},
			dbName:  "foo",
			options: Param("foo", 123),
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := test.options
			if opts == nil {
				opts = mock.NilOption
			}
			err := test.client.CreateDB(context.Background(), test.dbName, opts)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDestroyDB(t *testing.T) {
	tests := []struct {
		name    string
		client  *Client
		dbName  string
		options Option
		status  int
		err     string
	}{
		{
			name: "db error",
			client: &Client{
				driverClient: &mock.Client{
					DestroyDBFunc: func(context.Context, string, driver.Options) error {
						return errors.New("db error")
					},
				},
			},
			status: http.StatusInternalServerError,
			err:    "db error",
		},
		{
			name: "success",
			client: &Client{
				driverClient: &mock.Client{
					DestroyDBFunc: func(_ context.Context, dbName string, options driver.Options) error {
						expectedDBName := "foo"
						gotOpts := map[string]interface{}{}
						options.Apply(gotOpts)
						expectedOpts := map[string]interface{}{"foo": 123}
						if dbName != expectedDBName {
							return fmt.Errorf("Unexpected dbname: %s", dbName)
						}
						if d := testy.DiffInterface(expectedOpts, gotOpts); d != nil {
							return fmt.Errorf("Unexpected opts:\n%s", d)
						}
						return nil
					},
				},
			},
			dbName:  "foo",
			options: Param("foo", 123),
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := test.options
			if opts == nil {
				opts = mock.NilOption
			}
			err := test.client.DestroyDB(context.Background(), test.dbName, opts)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDBsStats(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		dbnames  []string
		expected []*DBStats
		err      string
		status   int
	}{
		{
			name: "fallback to old driver",
			client: &Client{
				driverClient: &mock.Client{
					DBFunc: func(name string, _ driver.Options) (driver.DB, error) {
						switch name {
						case "foo":
							return &mock.DB{
								StatsFunc: func(context.Context) (*driver.DBStats, error) {
									return &driver.DBStats{Name: "foo", DiskSize: 123}, nil
								},
							}, nil
						case "bar":
							return &mock.DB{
								StatsFunc: func(context.Context) (*driver.DBStats, error) {
									return &driver.DBStats{Name: "bar", DiskSize: 321}, nil
								},
							}, nil
						default:
							return nil, errors.New("not found")
						}
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			expected: []*DBStats{
				{Name: "foo", DiskSize: 123},
				{Name: "bar", DiskSize: 321},
			},
		},
		{
			name: "fallback due to old server",
			client: &Client{
				driverClient: &mock.Client{
					DBFunc: func(name string, _ driver.Options) (driver.DB, error) {
						switch name {
						case "foo":
							return &mock.DB{
								StatsFunc: func(context.Context) (*driver.DBStats, error) {
									return &driver.DBStats{Name: "foo", DiskSize: 123}, nil
								},
							}, nil
						case "bar":
							return &mock.DB{
								StatsFunc: func(context.Context) (*driver.DBStats, error) {
									return &driver.DBStats{Name: "bar", DiskSize: 321}, nil
								},
							}, nil
						default:
							return nil, errors.New("not found")
						}
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			expected: []*DBStats{
				{Name: "foo", DiskSize: 123},
				{Name: "bar", DiskSize: 321},
			},
		},
		{
			name: "native success",
			client: &Client{
				driverClient: &mock.DBsStatser{
					DBsStatsFunc: func(context.Context, []string) ([]*driver.DBStats, error) {
						return []*driver.DBStats{
							{Name: "foo", DiskSize: 123},
							{Name: "bar", DiskSize: 321},
						}, nil
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			expected: []*DBStats{
				{Name: "foo", DiskSize: 123},
				{Name: "bar", DiskSize: 321},
			},
		},
		{
			name: "native error",
			client: &Client{
				driverClient: &mock.DBsStatser{
					DBsStatsFunc: func(context.Context, []string) ([]*driver.DBStats, error) {
						return nil, errors.New("native failure")
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			err:     "native failure",
			status:  500,
		},
		{
			name: "fallback error",
			client: &Client{
				driverClient: &mock.Client{
					DBFunc: func(_ string, _ driver.Options) (driver.DB, error) {
						return &mock.DB{
							StatsFunc: func(context.Context) (*driver.DBStats, error) {
								return nil, errors.New("fallback failure")
							},
						}, nil
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			err:     "fallback failure",
			status:  http.StatusInternalServerError,
		},
		{
			name: "fallback db connect error",
			client: &Client{
				driverClient: &mock.Client{
					DBFunc: func(string, driver.Options) (driver.DB, error) {
						return nil, errors.New("db conn failure")
					},
				},
			},
			dbnames: []string{"foo", "bar"},
			err:     "db conn failure",
			status:  http.StatusInternalServerError,
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			status: http.StatusServiceUnavailable,
			err:    "kivik: client closed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats, err := test.client.DBsStats(context.Background(), test.dbnames)
			if d := internal.StatusErrorDiff(test.err, test.status, err); d != "" {
				t.Error(d)
			}
			if d := testy.DiffInterface(test.expected, stats); d != nil {
				t.Error(d)
			}
		})
	}
}

func TestPing(t *testing.T) {
	type pingTest struct {
		name     string
		client   *Client
		expected bool
		err      string
	}
	tests := []pingTest{
		{
			name: "non-pinger",
			client: &Client{
				driverClient: &mock.Client{
					VersionFunc: func(context.Context) (*driver.Version, error) {
						return &driver.Version{}, nil
					},
				},
			},
			expected: true,
		},
		{
			name: "pinger",
			client: &Client{
				driverClient: &mock.Pinger{
					PingFunc: func(context.Context) (bool, error) {
						return true, nil
					},
				},
			},
			expected: true,
		},
		{
			name: "closed",
			client: &Client{
				closed: true,
			},
			err: "kivik: client closed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.client.Ping(context.Background())
			if !testy.ErrorMatches(test.err, err) {
				t.Errorf("Unexpected error: %s", err)
			}
			if result != test.expected {
				t.Errorf("Unexpected result: %t", result)
			}
		})
	}
}

func TestClientClose(t *testing.T) {
	t.Parallel()

	type tst struct {
		client *Client
		err    string
	}
	tests := testy.NewTable()
	tests.Add("non-closer", tst{
		client: &Client{driverClient: &mock.Client{}},
	})
	tests.Add("error", tst{
		client: &Client{driverClient: &mock.ClientCloser{
			CloseFunc: func() error {
				return errors.New("close err")
			},
		}},
		err: "close err",
	})
	tests.Add("success", tst{
		client: &Client{driverClient: &mock.ClientCloser{
			CloseFunc: func() error {
				return nil
			},
		}},
	})

	tests.Run(t, func(t *testing.T, test tst) {
		t.Parallel()
		err := test.client.Close()
		if !testy.ErrorMatches(test.err, err) {
			t.Errorf("Unexpected error: %s", err)
		}
	})

	t.Run("blocks", func(t *testing.T) {
		t.Parallel()

		const delay = 100 * time.Millisecond

		type tt struct {
			client driver.Client
			work   func(*testing.T, *Client)
		}

		tests := testy.NewTable()
		tests.Add("AllDBs", tt{
			client: &mock.Client{
				AllDBsFunc: func(context.Context, driver.Options) ([]string, error) {
					time.Sleep(delay)
					return nil, nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_, _ = c.AllDBs(context.Background())
			},
		})
		tests.Add("DBExists", tt{
			client: &mock.Client{
				DBExistsFunc: func(context.Context, string, driver.Options) (bool, error) {
					time.Sleep(delay)
					return true, nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_, _ = c.DBExists(context.Background(), "x")
			},
		})
		tests.Add("CreateDB", tt{
			client: &mock.Client{
				CreateDBFunc: func(context.Context, string, driver.Options) error {
					time.Sleep(delay)
					return nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_ = c.CreateDB(context.Background(), "x")
			},
		})
		tests.Add("DestroyDB", tt{
			client: &mock.Client{
				DestroyDBFunc: func(context.Context, string, driver.Options) error {
					time.Sleep(delay)
					return nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_ = c.DestroyDB(context.Background(), "x")
			},
		})
		tests.Add("DBsStats", tt{
			client: &mock.DBsStatser{
				DBsStatsFunc: func(context.Context, []string) ([]*driver.DBStats, error) {
					time.Sleep(delay)
					return nil, nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_, _ = c.DBsStats(context.Background(), nil)
			},
		})
		tests.Add("Ping", tt{
			client: &mock.Pinger{
				PingFunc: func(context.Context) (bool, error) {
					time.Sleep(delay)
					return true, nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_, _ = c.Ping(context.Background())
			},
		})
		tests.Add("Version", tt{
			client: &mock.Client{
				VersionFunc: func(context.Context) (*driver.Version, error) {
					time.Sleep(delay)
					return &driver.Version{}, nil
				},
			},
			work: func(_ *testing.T, c *Client) {
				_, _ = c.Version(context.Background())
			},
		})
		tests.Add("DBUpdates", tt{
			client: &mock.DBUpdater{
				DBUpdatesFunc: func(context.Context, driver.Options) (driver.DBUpdates, error) {
					return &mock.DBUpdates{
						NextFunc: func(*driver.DBUpdate) error {
							time.Sleep(delay)
							return io.EOF
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DBUpdates(context.Background())
				if err := u.Err(); err != nil {
					t.Fatal(err)
				}
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("AllDocs", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.DB{
						AllDocsFunc: func(context.Context, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").AllDocs(context.Background())
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("BulkDocs", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.BulkDocer{
						BulkDocsFunc: func(context.Context, []interface{}, driver.Options) ([]driver.BulkResult, error) {
							time.Sleep(delay)
							return nil, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				_, err := c.DB("foo").BulkDocs(context.Background(), []interface{}{
					map[string]string{"_id": "foo"},
				})
				if err != nil {
					t.Fatal(err)
				}
			},
		})
		tests.Add("BulkGet", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.BulkGetter{
						BulkGetFunc: func(context.Context, []driver.BulkGetReference, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").BulkGet(context.Background(), []BulkGetReference{})
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("Changes", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.DB{
						ChangesFunc: func(context.Context, driver.Options) (driver.Changes, error) {
							return &mock.Changes{
								NextFunc: func(*driver.Change) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").Changes(context.Background())
				if err := u.Err(); err != nil {
					t.Fatal(err)
				}
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("DesignDocs", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.DesignDocer{
						DesignDocsFunc: func(context.Context, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").DesignDocs(context.Background())
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("LocalDocs", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.LocalDocer{
						LocalDocsFunc: func(context.Context, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").LocalDocs(context.Background())
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("Query", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.DB{
						QueryFunc: func(context.Context, string, string, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").Query(context.Background(), "", "")
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("Find", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.Finder{
						FindFunc: func(context.Context, interface{}, driver.Options) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").Find(context.Background(), nil)
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("Get", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.DB{
						GetFunc: func(context.Context, string, driver.Options) (*driver.Document, error) {
							time.Sleep(delay)
							return &driver.Document{}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").Get(context.Background(), "")
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})
		tests.Add("RevsDiff", tt{
			client: &mock.Client{
				DBFunc: func(string, driver.Options) (driver.DB, error) {
					return &mock.RevsDiffer{
						RevsDiffFunc: func(context.Context, interface{}) (driver.Rows, error) {
							return &mock.Rows{
								NextFunc: func(*driver.Row) error {
									time.Sleep(delay)
									return io.EOF
								},
							}, nil
						},
					}, nil
				},
			},
			work: func(t *testing.T, c *Client) { //nolint:thelper // Not a helper
				u := c.DB("foo").RevsDiff(context.Background(), "")
				for u.Next() { //nolint:revive // intentional empty block
				}
				if u.Err() != nil {
					t.Fatal(u.Err())
				}
			},
		})

		tests.Run(t, func(t *testing.T, tt tt) {
			t.Parallel()

			c := &Client{
				driverClient: tt.client,
			}

			start := time.Now()
			tt.work(t, c)
			time.Sleep(delay / 10)
			_ = c.Close()
			if elapsed := time.Since(start); elapsed < delay {
				t.Errorf("client.Close() didn't block long enough (%v < %v)", elapsed, delay)
			}
		})
	})
}
