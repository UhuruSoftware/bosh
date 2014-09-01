package commands

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"

	bosherr "bosh/errors"
	boshsys "bosh/system"
)

type gotarballCompressor struct {
	fs boshsys.FileSystem
}

func NewGoTarballCompressor(
	fs boshsys.FileSystem,
) gotarballCompressor {
	return gotarballCompressor{fs: fs}
}

func (c gotarballCompressor) CompressFilesInDir(dir string) (string, error) {
	tarball, err := c.fs.TempFile("bosh-platform-disk-TarballCompressor-CompressFilesInDir")
	if err != nil {
		return "", bosherr.WrapError(err, "Creating temporary file for tarball")
	}

	tarballPath := tarball.Name()

	err = compress(tarballPath, dir)
	if err != nil {
		return "", bosherr.WrapError(err, "Compressing folder")
	}

	return tarballPath, nil
}

func (c gotarballCompressor) DecompressFileToDir(tarballPath string, dir string) error {
	//check if destination exists
	_, err := os.Stat(dir)
	if err != nil {
		return err
	}

	err = uncompress(dir, tarballPath)
	if err != nil {
		return bosherr.WrapError(err, "Uncompressing file")
	}

	return nil
}

func (c gotarballCompressor) CleanUp(tarballPath string) error {
	return c.fs.RemoveAll(tarballPath)
}

func compress(outFilePath string, inPath string) error {

	fw, err := os.Create(outFilePath)
	if err != nil {
		return err
	}
	defer fw.Close()
	// gzip write
	gw := gzip.NewWriter(fw)
	defer gw.Close()

	// tar write
	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.Walk(inPath, func(path string, fileInfo os.FileInfo, _ error) error {
		if inPath != path {

			archivePath, err := filepath.Rel(inPath, path)

			if err != nil {
				return err
			}

			//ToSlash needed for windows/linux compatibility
			err = writeTarGz(path, filepath.ToSlash(archivePath), tw, fileInfo)
			if err != nil {
				return err
			}

		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil

}

func writeTarGz(path string, archivePath string, tw *tar.Writer, fileInfo os.FileInfo) error {

	fr, err := os.Open(path)
	if err != nil {
		return err
	}

	defer fr.Close()

	header := new(tar.Header)
	if fileInfo.IsDir() {
		archivePath = archivePath + "/"
		//TODO : windows does not have folder permisions
		header.Mode = 1021
	} else {

		header.Size = fileInfo.Size()
		header.Mode = int64(fileInfo.Mode())
	}

	header.Name = archivePath
	header.ModTime = fileInfo.ModTime()

	err = tw.WriteHeader(header)
	if err != nil {
		return err
	}

	if !fileInfo.IsDir() {
		_, err = io.Copy(tw, fr)
		if err != nil {
			return err
		}
	}

	return nil
}

func uncompress(path string, tarFile string) error {

	file, err := os.Open(tarFile)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	if err != nil {
		return err
	}

	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			} else {
				return err
			}
		}
		writeFromTarGz(header, path, tarReader)

	}

	return err
}

func writeFromTarGz(header *tar.Header, path string, tarReader *tar.Reader) error {

	uFilePath := filepath.Join(path, header.Name)

	if strings.HasSuffix(header.Name, "/") {
		err := os.MkdirAll(uFilePath, header.FileInfo().Mode())
		if err != nil {
			return err
		}
	} else {
		fileSize := header.Size + 1
		content := make([]byte, fileSize)

		_, err := tarReader.Read(content)
		if err != nil {
			if err == io.EOF {
				//do nothing - possible 0 kb file
			} else {
				return err
			}
		}

		uFile, err := os.Create(uFilePath)

		if err != nil {
			return err
		}
		defer uFile.Close()

		err = uFile.Chmod(header.FileInfo().Mode())

		_, err = uFile.Write(content)
		if err != nil {
			return err
		}

	}
	return nil
}
