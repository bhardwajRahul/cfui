package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// TunnelManagement stores the singleton remote tunnel-management credentials.
type TunnelManagement struct {
	ent.Schema
}

// Fields of the TunnelManagement.
func (TunnelManagement) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").NotEmpty().Unique(),
		field.Bool("enabled").Default(false),
		field.String("account_id").Default(""),
		field.String("tunnel_id").Default(""),
		field.String("api_token").Default(""),
		field.String("api_email").Default(""),
		field.String("api_key").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
