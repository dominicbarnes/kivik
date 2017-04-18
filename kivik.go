package kivik

import (
	"context"
	"fmt"
	"regexp"

	"github.com/flimzy/kivik/driver"
	"github.com/flimzy/kivik/errors"
	"github.com/imdario/mergo"
)

// Client is a client connection handle to a CouchDB-like server.
type Client struct {
	dsn          string
	driverName   string
	driverClient driver.Client
}

// Options is a collection of options. The keys and values are backend specific.
type Options map[string]interface{}

func mergeOptions(otherOpts ...Options) (Options, error) {
	var options Options
	for _, opts := range otherOpts {
		if err := mergo.MergeWithOverwrite(&options, opts); err != nil {
			return nil, err
		}
	}
	return options, nil
}

// New creates a new client object specified by its database driver name
// and a driver-specific data source name.
func New(ctx context.Context, driverName, dataSourceName string) (*Client, error) {
	driversMu.RLock()
	driveri, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("kivik: unknown driver %q (forgotten import?)", driverName)
	}
	client, err := driveri.NewClient(ctx, dataSourceName)
	if err != nil {
		return nil, err
	}
	return &Client{
		dsn:          dataSourceName,
		driverName:   driverName,
		driverClient: client,
	}, nil
}

// Driver returns the name of the driver string used to connect this client.
func (c *Client) Driver() string {
	return c.driverName
}

// DSN returns the data source name used to connect this client.
func (c *Client) DSN() string {
	return c.dsn
}

// ServerInfo returns version and vendor info about the backend.
func (c *Client) ServerInfo(ctx context.Context, options ...Options) (driver.ServerInfo, error) {
	opts, err := mergeOptions(options...)
	if err != nil {
		return nil, err
	}
	return c.driverClient.ServerInfo(ctx, opts)
}

// DB returns a handle to the requested database. Any options parameters
// passed are merged, with later values taking precidence.
func (c *Client) DB(ctx context.Context, dbName string, options ...Options) (*DB, error) {
	opts, err := mergeOptions(options...)
	if err != nil {
		return nil, err
	}
	db, err := c.driverClient.DB(ctx, dbName, opts)
	return &DB{
		driverDB: db,
	}, err
}

// AllDBs returns a list of all databases.
func (c *Client) AllDBs(ctx context.Context, options ...Options) ([]string, error) {
	opts, err := mergeOptions(options...)
	if err != nil {
		return nil, err
	}
	return c.driverClient.AllDBs(ctx, opts)
}

// DBExists returns true if the specified database exists.
func (c *Client) DBExists(ctx context.Context, dbName string, options ...Options) (bool, error) {
	opts, err := mergeOptions(options...)
	if err != nil {
		return false, err
	}
	return c.driverClient.DBExists(ctx, dbName, opts)
}

// Copied verbatim from http://docs.couchdb.org/en/2.0.0/api/database/common.html#head--db
var validDBName = regexp.MustCompile("^[a-z][a-z0-9_$()+/-]*$")

// CreateDB creates a DB of the requested name.
func (c *Client) CreateDB(ctx context.Context, dbName string, options ...Options) error {
	opts, err := mergeOptions(options...)
	if err != nil {
		return err
	}
	if !validDBName.MatchString(dbName) {
		return errors.Status(StatusBadRequest, "invalid database name")
	}
	return c.driverClient.CreateDB(ctx, dbName, opts)
}

// DestroyDB deletes the requested DB.
func (c *Client) DestroyDB(ctx context.Context, dbName string, options ...Options) error {
	opts, err := mergeOptions(options...)
	if err != nil {
		return err
	}
	return c.driverClient.DestroyDB(ctx, dbName, opts)
}

// Authenticate authenticates the client with the passed authenticator, which
// is driver-specific. If the driver does not understand the authenticator, an
// error will be returned.
func (c *Client) Authenticate(ctx context.Context, a interface{}) error {
	if auth, ok := c.driverClient.(driver.Authenticator); ok {
		return auth.Authenticate(ctx, a)
	}
	return ErrNotImplemented
}
