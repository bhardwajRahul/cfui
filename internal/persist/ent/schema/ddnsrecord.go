package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DDNSRecord stores ordered DDNS record definitions.
type DDNSRecord struct {
	ent.Schema
}

// Fields of the DDNSRecord.
func (DDNSRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("settings_key").NotEmpty().Default("default"),
		field.Int("sort_order").NonNegative(),
		field.String("name").NotEmpty(),
		field.String("zone_id").NotEmpty(),
		field.String("zone_name").Default(""),
		field.String("type").NotEmpty(),
		field.String("value").Default(""),
		field.String("comment").Default("cfui"),
		field.Bool("proxied").Default(false),
		field.Int("ttl").Default(1),
	}
}

// Indexes of the DDNSRecord.
func (DDNSRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("settings_key", "sort_order").Unique(),
	}
}
