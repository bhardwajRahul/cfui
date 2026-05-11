package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DDNSIPSource stores ordered DDNS public IP source endpoints.
type DDNSIPSource struct {
	ent.Schema
}

// Fields of the DDNSIPSource.
func (DDNSIPSource) Fields() []ent.Field {
	return []ent.Field{
		field.String("settings_key").NotEmpty().Default("default"),
		field.Int("sort_order").NonNegative(),
		field.String("url").NotEmpty(),
		field.String("ip_type").Default("auto"),
	}
}

// Indexes of the DDNSIPSource.
func (DDNSIPSource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("settings_key", "sort_order").Unique(),
	}
}
