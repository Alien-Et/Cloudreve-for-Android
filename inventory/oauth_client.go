package inventory

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/oauthclient"
	"github.com/cloudreve/Cloudreve/v4/ent/oauthgrant"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	"github.com/cloudreve/Cloudreve/v4/pkg/util"
	"github.com/gofrs/uuid"
)

type (
	OAuthClientClient interface {
		TxOperator
		// GetByGUID returns the OAuth client by its GUID (client_id).
		GetByGUID(ctx context.Context, guid string) (*ent.OAuthClient, error)
		// GetByGUIDWithGrants returns the OAuth client by its GUID (client_id) with the grants for the user.
		GetByGUIDWithGrants(ctx context.Context, guid string, uid int) (*ent.OAuthClient, error)
		// UpsertGrant creates or updates an OAuth grant for a user and client.
		UpsertGrant(ctx context.Context, userID, clientID int, scopes []string) error
		// UpdateGrantLastUsedAt updates the last used at for an OAuth grant for a user and client.
		UpdateGrantLastUsedAt(ctx context.Context, userID, clientID int) error
		// DeleteGrantByUserAndClientGUID deletes an OAuth grant for a user by the client GUID.
		// Returns true if the grant was deleted, false if it was not found.
		DeleteGrantByUserAndClientGUID(ctx context.Context, userID int, clientGUID string) (bool, error)
		// List returns a paginated list of OAuth clients.
		List(ctx context.Context, args *ListOAuthClientArgs) (*ListOAuthClientResult, error)
		// GetByID returns the OAuth client by its ID.
		GetByID(ctx context.Context, id int) (*ent.OAuthClient, error)
		// Create creates a new OAuth client.
		Create(ctx context.Context, client *ent.OAuthClient) (*ent.OAuthClient, error)
		// Update updates an existing OAuth client.
		Update(ctx context.Context, client *ent.OAuthClient) (*ent.OAuthClient, error)
		// Delete deletes an OAuth client by its ID.
		Delete(ctx context.Context, id int) error
		// CountGrants returns the number of grants for an OAuth client.
		CountGrants(ctx context.Context, id int) (int, error)
		// GetGrantsByUserID returns the OAuth grants for a user.
		GetGrantsByUserID(ctx context.Context, userID int) ([]*ent.OAuthGrant, error)
	}

	ListOAuthClientArgs struct {
		*PaginationArgs
		Name      string
		IsEnabled *bool
	}

	ListOAuthClientResult struct {
		*PaginationResults
		Clients []*ent.OAuthClient
	}

	LoadOAuthGrantClient struct{}
)

func NewOAuthClientClient(client *ent.Client, dbType conf.DBType) OAuthClientClient {
	return &oauthClientClient{
		client:      client,
		maxSQlParam: sqlParamLimit(dbType),
	}
}

type oauthClientClient struct {
	client      *ent.Client
	maxSQlParam int
}

func (c *oauthClientClient) SetClient(newClient *ent.Client) TxOperator {
	return &oauthClientClient{client: newClient, maxSQlParam: c.maxSQlParam}
}

func (c *oauthClientClient) GetClient() *ent.Client {
	return c.client
}

func (c *oauthClientClient) GetByGUID(ctx context.Context, guid string) (*ent.OAuthClient, error) {
	return c.client.OAuthClient.Query().
		Where(oauthclient.GUID(guid), oauthclient.IsEnabled(true)).
		First(ctx)
}

func (c *oauthClientClient) GetByGUIDWithGrants(ctx context.Context, guid string, uid int) (*ent.OAuthClient, error) {
	stm := c.client.OAuthClient.Query().
		Where(oauthclient.GUID(guid), oauthclient.IsEnabled(true))
	if uid > 0 {
		stm.WithGrants(func(ogq *ent.OAuthGrantQuery) {
			ogq.Where(oauthgrant.UserID(uid))
		})
	}

	return stm.First(ctx)
}

func (c *oauthClientClient) UpsertGrant(ctx context.Context, userID, clientID int, scopes []string) error {
	return c.client.OAuthGrant.Create().
		SetUserID(userID).
		SetClientID(clientID).
		SetScopes(scopes).
		SetLastUsedAt(time.Now()).
		OnConflict(
			sql.ConflictColumns(oauthgrant.FieldUserID, oauthgrant.FieldClientID),
		).
		UpdateScopes().
		UpdateLastUsedAt().
		Exec(ctx)
}

func (c *oauthClientClient) UpdateGrantLastUsedAt(ctx context.Context, userID, clientID int) error {
	return c.client.OAuthGrant.Update().
		Where(oauthgrant.UserID(userID), oauthgrant.ClientID(clientID)).
		SetLastUsedAt(time.Now()).
		Exec(ctx)
}

func (c *oauthClientClient) GetGrantsByUserID(ctx context.Context, userID int) ([]*ent.OAuthGrant, error) {
	return withOAuthGrantEagerLoadings(ctx, c.client.OAuthGrant.Query()).
		Where(oauthgrant.UserID(userID)).
		All(ctx)
}

func (c *oauthClientClient) DeleteGrantByUserAndClientGUID(ctx context.Context, userID int, clientGUID string) (bool, error) {
	// First, get the client by GUID to get its ID
	client, err := c.client.OAuthClient.Query().
		Where(oauthclient.GUID(clientGUID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get OAuth client: %w", err)
	}

	// Delete the grant for this user and client
	deleted, err := c.client.OAuthGrant.Delete().
		Where(oauthgrant.UserID(userID), oauthgrant.ClientID(client.ID)).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to delete OAuth grant: %w", err)
	}

	return deleted > 0, nil
}

func (c *oauthClientClient) List(ctx context.Context, args *ListOAuthClientArgs) (*ListOAuthClientResult, error) {
	query := c.client.OAuthClient.Query()

	if args.Name != "" {
		query.Where(oauthclient.NameContains(args.Name))
	}

	if args.IsEnabled != nil {
		query.Where(oauthclient.IsEnabled(*args.IsEnabled))
	}

	pageSize := capPageSize(c.maxSQlParam, args.PageSize, 1)

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count OAuth clients: %w", err)
	}

	query.Order(getOAuthClientOrderOption(args)...)

	clients, err := query.
		Limit(pageSize).
		Offset(args.Page * pageSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list OAuth clients: %w", err)
	}

	return &ListOAuthClientResult{
		PaginationResults: &PaginationResults{
			TotalItems: total,
			Page:       args.Page,
			PageSize:   pageSize,
		},
		Clients: clients,
	}, nil
}

func (c *oauthClientClient) GetByID(ctx context.Context, id int) (*ent.OAuthClient, error) {
	return c.client.OAuthClient.Query().
		Where(oauthclient.ID(id)).
		First(ctx)
}

func (c *oauthClientClient) Create(ctx context.Context, client *ent.OAuthClient) (*ent.OAuthClient, error) {
	if client.Props == nil {
		client.Props = &types.OAuthClientProps{}
	}

	// Generate a new GUID and secret if not provided
	if client.GUID == "" {
		client.GUID = uuid.Must(uuid.NewV4()).String()
	}
	if client.Secret == "" {
		client.Secret = util.RandStringRunesCrypto(32)
	}

	return c.client.OAuthClient.Create().
		SetGUID(client.GUID).
		SetSecret(client.Secret).
		SetName(client.Name).
		SetHomepageURL(client.HomepageURL).
		SetRedirectUris(client.RedirectUris).
		SetScopes(client.Scopes).
		SetProps(client.Props).
		SetIsEnabled(client.IsEnabled).
		Save(ctx)
}

func (c *oauthClientClient) Update(ctx context.Context, client *ent.OAuthClient) (*ent.OAuthClient, error) {
	if client.Props == nil {
		client.Props = &types.OAuthClientProps{}
	}

	update := c.client.OAuthClient.UpdateOneID(client.ID).
		SetName(client.Name).
		SetHomepageURL(client.HomepageURL).
		SetRedirectUris(client.RedirectUris).
		SetScopes(client.Scopes).
		SetProps(client.Props).
		SetIsEnabled(client.IsEnabled)

	// Only update secret if provided (non-empty)
	if client.Secret != "" {
		update.SetSecret(client.Secret)
	}

	return update.Save(ctx)
}

func (c *oauthClientClient) Delete(ctx context.Context, id int) error {
	// Delete all grants first
	_, err := c.client.OAuthGrant.Delete().
		Where(oauthgrant.ClientID(id)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth grants: %w", err)
	}

	// Delete the client
	return c.client.OAuthClient.DeleteOneID(id).Exec(ctx)
}

func (c *oauthClientClient) CountGrants(ctx context.Context, id int) (int, error) {
	return c.client.OAuthGrant.Query().
		Where(oauthgrant.ClientID(id)).
		Count(ctx)
}

func getOAuthClientOrderOption(args *ListOAuthClientArgs) []oauthclient.OrderOption {
	orderTerm := getOrderTerm(args.Order)
	switch args.OrderBy {
	case oauthclient.FieldName:
		return []oauthclient.OrderOption{oauthclient.ByName(orderTerm), oauthclient.ByID(orderTerm)}
	case oauthclient.FieldCreatedAt:
		return []oauthclient.OrderOption{oauthclient.ByCreatedAt(orderTerm), oauthclient.ByID(orderTerm)}
	case oauthclient.FieldIsEnabled:
		return []oauthclient.OrderOption{oauthclient.ByIsEnabled(orderTerm), oauthclient.ByID(orderTerm)}
	default:
		return []oauthclient.OrderOption{oauthclient.ByID(orderTerm)}
	}
}

func withOAuthGrantEagerLoadings(ctx context.Context, q *ent.OAuthGrantQuery) *ent.OAuthGrantQuery {
	if v, ok := ctx.Value(LoadOAuthGrantClient{}).(bool); ok && v {
		q.WithClient(func(ocq *ent.OAuthClientQuery) {
		})
	}

	return q
}
