package s3dav

import (
	"context"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	aferos3 "github.com/fclairamb/afero-s3"
	"github.com/spf13/afero"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type FSFactory func(context.Context, FSConfig, Credentials) (afero.Fs, error)

func newS3FS(_ context.Context, cfg FSConfig, creds Credentials) (afero.Fs, error) {
	awsCfg := aws.Config{
		Region: cfg.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			"",
		)),
		EndpointResolverWithOptions: aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				PartitionID:       "aws",
				URL:               cfg.Endpoint,
				SigningRegion:     cfg.Region,
				HostnameImmutable: true,
			}, nil
		}),
	}
	client := awss3.NewFromConfig(awsCfg, func(options *awss3.Options) {
		options.UsePathStyle = cfg.PathStyle
	})
	fs := aferos3.NewFsFromClient(cfg.BucketName, client)
	return s3ObjectKeyFS{Fs: fs, rootPrefix: cfg.RootPrefix}, nil
}

type s3ObjectKeyFS struct {
	afero.Fs
	rootPrefix string
}

func (fs s3ObjectKeyFS) s3Path(name string) string {
	rootPrefix := strings.Trim(fs.rootPrefix, "/")
	key := strings.TrimLeft(name, "/")
	if key == "" {
		if rootPrefix != "" {
			return rootPrefix
		}
		return "/"
	}
	if rootPrefix == "" {
		return key
	}
	return path.Join(rootPrefix, key)
}

func (fs s3ObjectKeyFS) Create(name string) (afero.File, error) {
	return fs.Fs.Create(fs.s3Path(name))
}

func (fs s3ObjectKeyFS) Chmod(name string, mode os.FileMode) error {
	return fs.Fs.Chmod(fs.s3Path(name), mode)
}

func (fs s3ObjectKeyFS) Chown(name string, uid, gid int) error {
	return fs.Fs.Chown(fs.s3Path(name), uid, gid)
}

func (fs s3ObjectKeyFS) Chtimes(name string, atime, mtime time.Time) error {
	return fs.Fs.Chtimes(fs.s3Path(name), atime, mtime)
}

func (fs s3ObjectKeyFS) Mkdir(name string, perm os.FileMode) error {
	return fs.Fs.Mkdir(fs.s3Path(name), perm)
}

func (fs s3ObjectKeyFS) MkdirAll(name string, perm os.FileMode) error {
	return fs.Fs.MkdirAll(fs.s3Path(name), perm)
}

func (fs s3ObjectKeyFS) Open(name string) (afero.File, error) {
	return fs.Fs.Open(fs.s3Path(name))
}

func (fs s3ObjectKeyFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return fs.Fs.OpenFile(fs.s3Path(name), flag, perm)
}

func (fs s3ObjectKeyFS) Remove(name string) error {
	return fs.Fs.Remove(fs.s3Path(name))
}

func (fs s3ObjectKeyFS) RemoveAll(name string) error {
	return fs.Fs.RemoveAll(fs.s3Path(name))
}

func (fs s3ObjectKeyFS) Rename(oldname, newname string) error {
	return fs.Fs.Rename(fs.s3Path(oldname), fs.s3Path(newname))
}

func (fs s3ObjectKeyFS) Stat(name string) (os.FileInfo, error) {
	return fs.Fs.Stat(fs.s3Path(name))
}

func listFiles(fs afero.Fs, rawPath string) (FilesResponse, error) {
	cleaned, err := CleanPath(rawPath, false)
	if err != nil {
		return FilesResponse{}, err
	}
	file, err := fs.Open(cleaned)
	if err != nil {
		return FilesResponse{}, err
	}
	defer file.Close()

	infos, err := file.Readdir(0)
	if err != nil {
		return FilesResponse{}, err
	}
	entries := make([]FileEntry, 0, len(infos))
	for _, info := range infos {
		name, ok := listEntryName(info.Name())
		if !ok {
			continue
		}
		p := JoinPath(cleaned, name)
		if info.IsDir() && !strings.HasSuffix(p, "/") {
			p += "/"
		}
		entries = append(entries, FileEntry{
			Name:    name,
			Path:    p,
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return FilesResponse{
		Path:    cleaned,
		Parent:  ParentPath(cleaned),
		Entries: entries,
	}, nil
}

func listEntryName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	name = strings.Trim(name, "/")
	if name == "" || name == "." {
		return "", false
	}
	if strings.Contains(name, "/") {
		name = path.Base(name)
	}
	if name == "" || name == "." || name == "/" {
		return "", false
	}
	return name, true
}

func writeFile(fs afero.Fs, rawPath string, body io.Reader) error {
	cleaned, err := CleanPath(rawPath, true)
	if err != nil {
		return err
	}
	file, err := fs.OpenFile(cleaned, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func openFile(fs afero.Fs, rawPath string) (afero.File, os.FileInfo, error) {
	cleaned, err := CleanPath(rawPath, true)
	if err != nil {
		return nil, nil, err
	}
	info, err := fs.Stat(cleaned)
	if err != nil {
		return nil, nil, err
	}
	file, err := fs.Open(cleaned)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

func deletePath(fs afero.Fs, rawPath string) error {
	cleaned, err := CleanPath(rawPath, true)
	if err != nil {
		return err
	}
	info, err := fs.Stat(cleaned)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fs.RemoveAll(cleaned)
	}
	return fs.Remove(cleaned)
}

func mkdir(fs afero.Fs, rawPath string) error {
	cleaned, err := CleanPath(rawPath, true)
	if err != nil {
		return err
	}
	return fs.MkdirAll(cleaned, 0755)
}

func renamePath(fs afero.Fs, from, to string) error {
	cleanFrom, err := CleanPath(from, true)
	if err != nil {
		return err
	}
	cleanTo, err := CleanPath(to, true)
	if err != nil {
		return err
	}
	return fs.Rename(cleanFrom, cleanTo)
}

func endpointFor(accountID, jurisdiction string) string {
	switch jurisdiction {
	case "eu":
		return "https://" + accountID + ".eu.r2.cloudflarestorage.com"
	case "fedramp":
		return "https://" + accountID + ".fedramp.r2.cloudflarestorage.com"
	default:
		return "https://" + accountID + ".r2.cloudflarestorage.com"
	}
}

func normalizeJurisdiction(v string) string {
	switch strings.TrimSpace(v) {
	case "eu", "fedramp":
		return strings.TrimSpace(v)
	default:
		return "default"
	}
}
