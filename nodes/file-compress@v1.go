package nodes

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"golang.org/x/exp/maps"
)

//go:embed file-compress@v1.yml
var fileZipDefinition string

type FileZipNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *FileZipNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	filePaths, err := core.InputValueById[[]string](c, n, ni.Core_file_compress_v1_Input_paths)
	if err != nil {
		return err
	}

	basePath, err := core.InputValueById[string](c, n, ni.Core_file_compress_v1_Input_base_path)
	if err != nil {
		return err
	}

	format, err := core.InputValueById[string](c, n, ni.Core_file_compress_v1_Input_format)
	if err != nil {
		return err
	}

	level, err := core.InputValueById[int](c, n, ni.Core_file_compress_v1_Input_level)
	if err != nil {
		return err
	}

	if !filepath.IsAbs(basePath) {
		cwd, err := os.Getwd()
		if err != nil {
			return core.CreateErr(c, err, "failed to get current working directory")
		}

		basePath = filepath.Join(cwd, basePath)
	}

	reader, err := createArchiveStreamFromPaths(c, filePaths, format, level, basePath)
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_file_compress_v1_Output_suffix, FormatToSuffix[format], core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	dsf := core.DataStreamFactory{
		Reader: reader,
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_file_compress_v1_Output_data, dsf, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_file_compress_v1_Output_exec_success, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(fileZipDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FileZipNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}

const (
	ZIP   string = "zip"
	TAR   string = "tar"
	TARGZ string = "targz"
)

var FormatToSuffix = map[string]string{
	ZIP:   ".zip",
	TAR:   ".tar",
	TARGZ: ".tar.gz",
}

func createArchiveStreamFromPaths(c *core.ExecutionState, itemPaths []string, compressionType string, compressionLevel int, basePath string) (io.Reader, error) {
	reader, writer := io.Pipe()

	if !filepath.IsAbs(basePath) {
		return nil, core.CreateErr(c, nil, "base path must be an absolute path")
	}

	if compressionLevel <= 0 {
		compressionLevel = gzip.NoCompression
	} else if compressionLevel >= gzip.BestCompression {
		compressionLevel = gzip.BestCompression
	}

	itemSet := make(map[string]os.FileInfo)
	dirSet := make(map[string]struct{})

	for _, path := range itemPaths {
		stats, err := os.Lstat(path)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to stat file: '%s'", path)
		}

		if stats.IsDir() {
			// ignore walking a directory that has already been walked
			if _, ok := dirSet[path]; ok {
				continue
			}

			tmpItemSet := make(map[string]os.FileInfo)
			path, err = walk(path, walkOpts{
				recursive: true,
				files:     true,
				dirs:      false,
			}, nil, tmpItemSet)
			if err != nil {
				return nil, core.CreateErr(c, err, "failed to walk directory: '%s'", path)
			}

			for k, v := range tmpItemSet {
				// ignore symlinks
				if v.Mode()&os.ModeSymlink != 0 {
					continue
				}

				dirSet[filepath.Dir(k)] = struct{}{}
				itemSet[k] = v
			}

		} else if stats.Mode().IsRegular() {
			itemSet[path] = stats
		}
	}

	itemSetSorted := maps.Keys(itemSet)
	sort.Strings(itemSetSorted)

	go func() {
		defer writer.Close()

		var err error
		switch compressionType {
		case "tar":
			err = writeTarArchive(itemSetSorted, writer, basePath)
		case "targz":
			gzipWriter, _ := gzip.NewWriterLevel(writer, compressionLevel)
			err = writeTarArchive(itemSetSorted, gzipWriter, basePath)
			gzipWriter.Close()
		case "zip":
			err = writeZipArchive(itemSetSorted, writer, basePath, compressionLevel)
		default:
			writer.CloseWithError(fmt.Errorf("unknown compression format: %s", compressionType))
			return
		}

		if err != nil {
			writer.CloseWithError(err)
		}
	}()

	return reader, nil
}

func writeTarArchive(paths []string, writer io.Writer, basePath string) error {
	tw := tar.NewWriter(writer)
	defer tw.Close()

	for _, absPath := range paths {
		file, err := os.Open(absPath)
		if err != nil {
			return err
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		relativePath := strings.TrimPrefix(absPath, basePath+string(os.PathSeparator))
		header.Name = friendlyPath(relativePath)

		err = tw.WriteHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(tw, file)
		if err != nil {
			return err
		}
	}
	return nil
}

func writeZipArchive(paths []string, writer io.Writer, basePath string, compressionLevel int) error {
	zw := zip.NewWriter(writer)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, compressionLevel)
	})

	for _, absPath := range paths {
		file, err := os.Open(absPath)
		if err != nil {
			return err
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		relativePath := strings.TrimPrefix(absPath, basePath+string(os.PathSeparator))
		header.Name = friendlyPath(relativePath)
		if compressionLevel == gzip.NoCompression {
			header.Method = zip.Store
		} else {
			header.Method = zip.Deflate
		}

		zf, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(zf, file)
		if err != nil {
			return err
		}
	}

	err := zw.Close()
	if err != nil {
		return err
	}
	return nil
}

func friendlyPath(path string) string {
	unixPath := path
	if runtime.GOOS == "windows" {
		unixPath = strings.ReplaceAll(path, "\\", "/")
	}

	// remove potential colons after the drive letter
	if len(unixPath) > 1 && unixPath[1] == ':' {
		unixPath = unixPath[0:1] + unixPath[2:]

		// convert the drive letter to lowercase
		if len(unixPath) > 0 {
			unixPath = strings.ToLower(unixPath[:1]) + unixPath[1:]
		}
	}

	// ensure the path is never absolute
	return strings.TrimLeft(unixPath, "/")
}
