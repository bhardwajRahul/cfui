package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// R2WebDAVSetting stores singleton Cloudflare R2 WebDAV settings.
type R2WebDAVSetting struct {
	ent.Schema
}

// Fields of the R2WebDAVSetting.
func (R2WebDAVSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").NotEmpty().Unique(),
		field.Bool("enabled").Default(false),
		field.String("account_id").Default(""),
		field.String("bucket_name").Default(""),
		field.String("jurisdiction").Default("default"),
		field.String("webdav_username").Default(""),
		field.String("webdav_password_hash").Default("").Sensitive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
