package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
)

// OAuthClient holds the schema definition for the OAuthClient entity.
type OAuthClient struct {
	ent.Schema
}

// Fields of the OAuthClient.
func (OAuthClient) Fields() []ent.Field {
	return []ent.Field{
		field.String("guid").
			MaxLen(255).
			Unique(),
		field.String("secret").
			MaxLen(255).
			Sensitive(),
		field.String("name").
			MaxLen(255),
		field.String("homepage_url").
			MaxLen(2048).
			Optional(),
		field.JSON("redirect_uris", []string{}).
			Default([]string{}),
		field.JSON("scopes", []string{}).
			Default([]string{}),
		field.JSON("props", &types.OAuthClientProps{}).
			Default(&types.OAuthClientProps{}),
		field.Bool("is_enabled").
			Default(true),
	}
}

// Edges of the OAuthClient.
func (OAuthClient) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("grants", OAuthGrant.Type),
	}
}

func (OAuthClient) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
