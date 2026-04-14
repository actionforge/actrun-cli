package nodes

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed file-decompress@v1.yml
var fileDecompressDefinition string

type FileDecompressNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *FileDecompressNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	reader, err := core.InputValueById[io.Reader](c, n, ni.Core_file_decompress_v1_Input_data)
	if err != nil {
		return err
	}

	defer utils.SafeCloseReaderAndIgnoreError(reader)

	format, err := core.InputValueById[string](c, n, ni.Core_file_decompress_v1_Input_format)
	if err != nil {
		return err
	}

	destPath, err := core.InputValueById[string](c, n, ni.Core_file_decompress_v1_Input_dest_path)
	if err != nil {
		return err
	}

	if !filepath.IsAbs(destPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return core.CreateErr(c, err, "failed to get current working directory")
		}
		destPath = filepath.Join(cwd, destPath)
	}

	cleanDest, pathErr := utils.ValidatePath(destPath)
	if pathErr != nil {
		return core.CreateErr(c, pathErr, "invalid destination path")
	}

	err = os.MkdirAll(cleanDest, 0o755)
	if err != nil {
		return core.CreateErr(c, err, "failed to create destination directory")
	}

	var extractedFiles []string

	switch format {
	case ZIP:
		extractedFiles, err = extractZipArchive(reader, cleanDest)
	case TAR:
		extractedFiles, err = extractTarArchive(reader, cleanDest)
	case TARGZ:
		gzReader, gzErr := gzip.NewReader(reader)
		if gzErr != nil {
			return core.CreateErr(c, gzErr, "failed to create gzip reader")
		}
		defer gzReader.Close()
		extractedFiles, err = extractTarArchive(gzReader, cleanDest)
	default:
		return core.CreateErr(c, nil, "unknown decompression format: %s", format)
	}

	if err != nil {
		execErr := n.Execute(ni.Core_file_decompress_v1_Output_exec_err, c, err)
		if execErr != nil {
			return execErr
		}
		return nil
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_file_decompress_v1_Output_files, extractedFiles, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_file_decompress_v1_Output_exec_success, c, nil)
	if err != nil {
		return err
	}

	return nil
}

// extractTarArchive reads a tar stream and extracts regular files to destPath.
// Symlinks are silently skipped.
func extractTarArchive(reader io.Reader, destPath string) ([]string, error) {
	tr := tar.NewReader(reader)
	var extractedFiles []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return extractedFiles, fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Skip symlinks
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			continue
		}

		target, err := sanitizeArchivePath(destPath, header.Name)
		if err != nil {
			return extractedFiles, err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return extractedFiles, fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return extractedFiles, fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.Create(target)
			if err != nil {
				return extractedFiles, fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return extractedFiles, fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
			extractedFiles = append(extractedFiles, target)
		}
	}

	return extractedFiles, nil
}

// extractZipArchive reads a zip stream and extracts regular files to destPath.
// Since zip requires random access, the stream is buffered into memory first.
// Symlinks are silently skipped.
func extractZipArchive(reader io.Reader, destPath string) ([]string, error) {
	// zip.NewReader requires io.ReaderAt + size, so buffer the stream
	buf, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read zip stream: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip archive: %w", err)
	}

	var extractedFiles []string

	for _, f := range zr.File {
		// Skip symlinks (mode check)
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		target, err := sanitizeArchivePath(destPath, f.Name)
		if err != nil {
			return extractedFiles, err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return extractedFiles, fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return extractedFiles, fmt.Errorf("failed to create parent directory: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			return extractedFiles, fmt.Errorf("failed to open zip entry: %w", err)
		}

		outFile, err := os.Create(target)
		if err != nil {
			rc.Close()
			return extractedFiles, fmt.Errorf("failed to create file: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			outFile.Close()
			rc.Close()
			return extractedFiles, fmt.Errorf("failed to write file: %w", err)
		}

		outFile.Close()
		rc.Close()
		extractedFiles = append(extractedFiles, target)
	}

	return extractedFiles, nil
}

// sanitizeArchivePath prevents zip-slip attacks by ensuring the resolved path
// stays within the destination directory.
func sanitizeArchivePath(destPath, entryName string) (string, error) {
	cleanName := filepath.FromSlash(entryName)
	target := filepath.Join(destPath, cleanName)

	// Prevent zip-slip: ensure the target is within destPath
	rel, err := filepath.Rel(destPath, target)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("illegal path in archive (zip-slip): %s", entryName)
	}

	return target, nil
}

func init() {
	err := core.RegisterNodeFactory(fileDecompressDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &FileDecompressNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
