package configbackup

import "cfui/internal/config"

func canonicalizePayload(payload Payload) (Payload, error) {
	if payload.Tunnels != nil {
		section := *payload.Tunnels
		section.ActiveKey = config.NormalizeTunnelKey(section.ActiveKey)
		section.Profiles = append([]TunnelProfile(nil), section.Profiles...)
		seen := make(map[string]bool, len(section.Profiles))
		for i := range section.Profiles {
			key := config.NormalizeTunnelKey(section.Profiles[i].Key)
			if key == "" || seen[key] {
				return Payload{}, ErrInvalidBackup
			}
			seen[key] = true
			section.Profiles[i].Key = key
		}
		payload.Tunnels = &section
	}

	if payload.RemoteManagement != nil {
		section := *payload.RemoteManagement
		section.Profiles = append([]RemoteProfile(nil), section.Profiles...)
		seen := make(map[string]bool, len(section.Profiles))
		for i := range section.Profiles {
			key := config.NormalizeTunnelKey(section.Profiles[i].Key)
			if key == "" || seen[key] {
				return Payload{}, ErrInvalidBackup
			}
			seen[key] = true
			section.Profiles[i].Key = key
		}
		payload.RemoteManagement = &section
	}

	if payload.S3WebDAV != nil {
		section := *payload.S3WebDAV
		section.ActiveKey = config.NormalizeS3MountKey(section.ActiveKey)
		section.Mounts = append([]S3Mount(nil), section.Mounts...)
		seen := make(map[string]bool, len(section.Mounts))
		for i := range section.Mounts {
			key := config.NormalizeS3MountKey(section.Mounts[i].Key)
			if key == "" || seen[key] {
				return Payload{}, ErrInvalidBackup
			}
			seen[key] = true
			section.Mounts[i].Key = key
		}
		payload.S3WebDAV = &section
	}

	if payload.Sensitive != nil {
		section := *payload.Sensitive
		var err error
		section.TunnelTokens, err = canonicalStringMap(section.TunnelTokens, config.NormalizeTunnelKey)
		if err != nil {
			return Payload{}, err
		}
		section.S3, err = canonicalCredentialsMap(section.S3)
		if err != nil {
			return Payload{}, err
		}
		payload.Sensitive = &section
	}

	return payload, nil
}

func canonicalStringMap(values map[string]string, normalize func(string) string) (map[string]string, error) {
	if values == nil {
		return nil, nil
	}
	canonical := make(map[string]string, len(values))
	for raw, value := range values {
		key := normalize(raw)
		if key == "" {
			return nil, ErrInvalidBackup
		}
		if _, exists := canonical[key]; exists {
			return nil, ErrInvalidBackup
		}
		canonical[key] = value
	}
	return canonical, nil
}

func canonicalCredentialsMap(values map[string]S3Credentials) (map[string]S3Credentials, error) {
	if values == nil {
		return nil, nil
	}
	canonical := make(map[string]S3Credentials, len(values))
	for raw, value := range values {
		key := config.NormalizeS3MountKey(raw)
		if key == "" {
			return nil, ErrInvalidBackup
		}
		if _, exists := canonical[key]; exists {
			return nil, ErrInvalidBackup
		}
		canonical[key] = value
	}
	return canonical, nil
}
