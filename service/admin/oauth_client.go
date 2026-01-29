package admin

import (
	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// System OAuth client GUIDs that cannot be deleted
var systemOAuthClientGUIDs = []string{
	inventory.OAuthClientDesktopGUID,
	inventory.OAuthClientiOSGUID,
}

type (
	SingleOAuthClientService struct {
		ID int `uri:"id" json:"id" binding:"required"`
	}
	SingleOAuthClientParamCtx struct{}
)

type (
	UpsertOAuthClientService struct {
		Client *ent.OAuthClient `json:"client" binding:"required"`
	}
	UpsertOAuthClientParamCtx struct{}
)

type (
	BatchOAuthClientService struct {
		IDs []int `json:"ids" binding:"required"`
	}
	BatchOAuthClientParamCtx struct{}
)

// OAuthClients lists OAuth clients with pagination
func (s *AdminListService) OAuthClients(c *gin.Context) (*ListOAuthClientResponse, error) {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	var isEnabled *bool
	if enabledStr, ok := s.Conditions["is_enabled"]; ok {
		enabled := enabledStr == "true"
		isEnabled = &enabled
	}

	res, err := oauthClient.List(c, &inventory.ListOAuthClientArgs{
		PaginationArgs: &inventory.PaginationArgs{
			Page:     s.Page - 1,
			PageSize: s.PageSize,
			OrderBy:  s.OrderBy,
			Order:    inventory.OrderDirection(s.OrderDirection),
		},
		Name:      s.Searches["name"],
		IsEnabled: isEnabled,
	})

	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list OAuth clients", err)
	}

	clients := lo.Map(res.Clients, func(client *ent.OAuthClient, _ int) GetOAuthClientResponse {
		return GetOAuthClientResponse{
			OAuthClient: client,
			IsSystem:    lo.Contains(systemOAuthClientGUIDs, client.GUID),
		}
	})

	return &ListOAuthClientResponse{
		Pagination: res.PaginationResults,
		Clients:    clients,
	}, nil
}

func (s *SingleOAuthClientService) Get(c *gin.Context) (*GetOAuthClientResponse, error) {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	client, err := oauthClient.GetByID(c, s.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "OAuth client not found", err)
	}

	res := &GetOAuthClientResponse{
		OAuthClient: client,
		IsSystem:    lo.Contains(systemOAuthClientGUIDs, client.GUID),
	}

	// Count grants
	grants, err := oauthClient.CountGrants(c, s.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count grants", err)
	}
	res.TotalGrants = grants

	return res, nil
}

func (s *UpsertOAuthClientService) Create(c *gin.Context) (*GetOAuthClientResponse, error) {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	if s.Client.ID > 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "ID must be 0 for creating new OAuth client", nil)
	}

	client, err := oauthClient.Create(c, s.Client)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create OAuth client", err)
	}

	service := &SingleOAuthClientService{ID: client.ID}
	return service.Get(c)
}

func (s *UpsertOAuthClientService) Update(c *gin.Context) (*GetOAuthClientResponse, error) {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	if s.Client.ID == 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "ID is required", nil)
	}

	// Check if this is a system client
	existing, err := oauthClient.GetByID(c, s.Client.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "OAuth client not found", err)
	}

	// System clients cannot change GUID
	if lo.Contains(systemOAuthClientGUIDs, existing.GUID) {
		s.Client.GUID = existing.GUID
	}

	_, err = oauthClient.Update(c, s.Client)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update OAuth client", err)
	}

	service := &SingleOAuthClientService{ID: s.Client.ID}
	return service.Get(c)
}

func (s *SingleOAuthClientService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	// Check if client exists
	client, err := oauthClient.GetByID(c, s.ID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "OAuth client not found", err)
	}

	// Check if this is a system client
	if lo.Contains(systemOAuthClientGUIDs, client.GUID) {
		return serializer.NewError(serializer.CodeInvalidActionOnSystemGroup, "Cannot delete system OAuth client", nil)
	}

	err = oauthClient.Delete(c, s.ID)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete OAuth client", err)
	}

	return nil
}

func (s *BatchOAuthClientService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	oauthClient := dep.OAuthClientClient()

	for _, id := range s.IDs {
		// Check if client exists
		client, err := oauthClient.GetByID(c, id)
		if err != nil {
			continue // Skip non-existent clients
		}

		// Check if this is a system client
		if lo.Contains(systemOAuthClientGUIDs, client.GUID) {
			continue // Skip system clients
		}

		// Delete the client (including grants)
		oauthClient.Delete(c, id)
	}

	return nil
}
