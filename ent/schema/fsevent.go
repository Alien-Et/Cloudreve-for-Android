package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/gofrs/uuid"
)

// FsEvent holds the schema definition for the FsEvent entity.
type FsEvent struct {
	ent.Schema
}

// Fields of the FsEvent.
func (FsEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Text("event"),
		field.UUID("subscriber", uuid.Must(uuid.NewV4())),
		field.Int("user_fsevent").Optional(),
	}
}

// Edges of the Task.
func (FsEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("fsevents").
			Field("user_fsevent").
			Unique(),
	}
}

func (FsEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
