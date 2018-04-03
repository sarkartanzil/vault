package database

import (
	"context"
	"fmt"

	"github.com/hashicorp/vault/logical"
	"github.com/hashicorp/vault/logical/framework"
)

const SecretCredsType = "creds"

func secretCreds(b *databaseBackend) *framework.Secret {
	return &framework.Secret{
		Type:   SecretCredsType,
		Fields: map[string]*framework.FieldSchema{},

		Renew:  b.secretCredsRenew(),
		Revoke: b.secretCredsRevoke(),
	}
}

func (b *databaseBackend) secretCredsRenew() framework.OperationFunc {
	return func(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
		// Get the username from the internal data
		usernameRaw, ok := req.Secret.InternalData["username"]
		if !ok {
			return nil, fmt.Errorf("secret is missing username internal data")
		}
		username, ok := usernameRaw.(string)

		roleNameRaw, ok := req.Secret.InternalData["role"]
		if !ok {
			return nil, fmt.Errorf("could not find role with name: %q", req.Secret.InternalData["role"])
		}

		role, err := b.Role(ctx, req.Storage, roleNameRaw.(string))
		if err != nil {
			return nil, err
		}
		if role == nil {
			return nil, fmt.Errorf("error during renew: could not find role with name %q", req.Secret.InternalData["role"])
		}

		f := framework.LeaseExtend(role.DefaultTTL, role.MaxTTL, b.System())
		resp, err := f(ctx, req, data)
		if err != nil {
			return nil, err
		}

		// Get the Database object
		db, err := b.GetConnection(ctx, req.Storage, role.DBName)
		if err != nil {
			return nil, err
		}

		db.RLock()
		defer db.RUnlock()

		// Make sure we increase the VALID UNTIL endpoint for this user.
		if expireTime := resp.Secret.ExpirationTime(); !expireTime.IsZero() {
			err := db.RenewUser(ctx, role.Statements, username, expireTime)
			if err != nil {
				b.CloseIfShutdown(db, err)
				return nil, err
			}
		}
		return resp, nil
	}
}

func (b *databaseBackend) secretCredsRevoke() framework.OperationFunc {
	return func(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
		// Get the username from the internal data
		usernameRaw, ok := req.Secret.InternalData["username"]
		if !ok {
			return nil, fmt.Errorf("secret is missing username internal data")
		}
		username, ok := usernameRaw.(string)

		var resp *logical.Response

		roleNameRaw, ok := req.Secret.InternalData["role"]
		if !ok {
			return nil, fmt.Errorf("no role name was provided")
		}

		role, err := b.Role(ctx, req.Storage, roleNameRaw.(string))
		if err != nil {
			return nil, err
		}
		if role == nil {
			return nil, fmt.Errorf("error during revoke: could not find role with name %q", req.Secret.InternalData["role"])
		}

		// Get our connection
		db, err := b.GetConnection(ctx, req.Storage, role.DBName)
		if err != nil {
			return nil, err
		}

		db.RLock()
		defer db.RUnlock()

		if err := db.RevokeUser(ctx, role.Statements, username); err != nil {
			b.CloseIfShutdown(db, err)
			return nil, err
		}
		return resp, nil
	}
}
