package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthGrant holds the schema definition for the OAuthGrant entity.
type OAuthGrant struct {
	ent.Schema
}

// Fields of the OAuthGrant.
func (OAuthGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("client_id"),
		field.JSON("scopes", []string{}).
			Default([]string{}),
		field.Time("last_used_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				dialect.MySQL: "datetime",
			}),
	}
}

// Edges of the OAuthGrant.
func (OAuthGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Field("user_id").
			Ref("oauth_grants").
			Unique().
			Required(),
		edge.From("client", OAuthClient.Type).
			Field("client_id").
			Ref("grants").
			Unique().
			Required(),
	}
}

func (OAuthGrant) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}

// Indexes of the OAuthGrant.
func (OAuthGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "client_id").
			Unique(),
	}
}
