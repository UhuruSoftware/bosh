package commands_test

import (
	"errors"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"

	. "bosh/platform/commands"
	fakesys "bosh/system/fakes"
)

func init() {
	Describe("GoTarBall", func() {
		It("compress/decompress files in dir", func() {
			fs, _ := getCompressorDependencies()
			gtc := NewGoTarballCompressor(fs)

			srcDir := fixtureSrcDir(GinkgoT())
			tgzName, err := gtc.CompressFilesInDir(srcDir)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(tgzName)

			dstDir := createdTmpDir(GinkgoT(), fs)
			defer os.RemoveAll(dstDir)
			err = gtc.DecompressFileToDir(tgzName, dstDir)
			Expect(err).ToNot(HaveOccurred())

			content, err := fs.ReadFileString(dstDir + "/app.stdout.log")
			Expect(err).ToNot(HaveOccurred())
			assert.Contains(GinkgoT(), content, "this is app stdout")

			content, err = fs.ReadFileString(dstDir + "/app.stderr.log")
			Expect(err).ToNot(HaveOccurred())
			assert.Contains(GinkgoT(), content, "this is app stderr")

			content, err = fs.ReadFileString(dstDir + "/other_logs/other_app.stdout.log")
			Expect(err).ToNot(HaveOccurred())
			assert.Contains(GinkgoT(), content, "this is other app stdout")
		})
		It("decompress file to dir returns error", func() {
			nonExistentDstDir := filepath.Join(os.TempDir(), "TestDecompressFileToDirReturnsError")

			fs, _ := getCompressorDependencies()
			gtc := NewGoTarballCompressor(fs)
			err := gtc.DecompressFileToDir(fixtureSrcTgz(GinkgoT()), nonExistentDstDir)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(nonExistentDstDir))
		})
	})
	Describe("GoTarBallCleanUp", func() {
		It("removes tarball path", func() {
			fs := fakesys.NewFakeFileSystem()
			gtc := NewGoTarballCompressor(fs)

			err := fs.WriteFileString("/fake-tarball.tar", "")
			Expect(err).ToNot(HaveOccurred())

			err = gtc.CleanUp("/fake-tarball.tar")
			Expect(err).ToNot(HaveOccurred())

			Expect(fs.FileExists("/fake-tarball.tar")).To(BeFalse())
		})

		It("returns error if removing tarball path fails", func() {
			fs := fakesys.NewFakeFileSystem()
			gtc := NewGoTarballCompressor(fs)

			fs.RemoveAllError = errors.New("fake-remove-all-err")

			err := gtc.CleanUp("/fake-tarball.tar")
			Expect(err).To(MatchError("fake-remove-all-err"))
		})
	})
}
