package sftpconfig

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ProgressFunc reports transfer progress for a single file.
type ProgressFunc func(transferred, total int64)

// UploadPath uploads a local file or directory to the remote path.
// Directories are transferred recursively.
func (c *SFTPClient) UploadPath(ctx context.Context, localPath, remotePath string, progress ProgressFunc) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("local path: %w", err)
	}
	if info.IsDir() {
		return c.uploadDir(ctx, localPath, remotePath, progress)
	}
	return c.UploadWithProgressCtx(ctx, localPath, remotePath, progress)
}

// DownloadPath downloads a remote file or directory to the local path.
// Directories are transferred recursively.
func (c *SFTPClient) DownloadPath(ctx context.Context, remotePath, localPath string, progress ProgressFunc) error {
	info, err := c.Stat(remotePath)
	if err != nil {
		return fmt.Errorf("remote path: %w", err)
	}
	if info.IsDir() {
		return c.downloadDir(ctx, remotePath, localPath, progress)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	return c.DownloadWithProgressCtx(ctx, remotePath, localPath, progress)
}

func (c *SFTPClient) uploadDir(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) error {
	if err := c.MkdirAll(remoteDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		src := filepath.Join(localDir, entry.Name())
		dst := path.Join(remoteDir, entry.Name())
		if entry.IsDir() {
			if err := c.uploadDir(ctx, src, dst, progress); err != nil {
				return err
			}
			continue
		}
		if err := c.UploadWithProgressCtx(ctx, src, dst, progress); err != nil {
			return err
		}
	}
	return nil
}

func (c *SFTPClient) downloadDir(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) error {
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return err
	}
	entries, err := c.ListDir(remoteDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		src := path.Join(remoteDir, entry.Name)
		dst := filepath.Join(localDir, entry.Name)
		if entry.IsDir {
			if err := c.downloadDir(ctx, src, dst, progress); err != nil {
				return err
			}
			continue
		}
		if err := c.DownloadWithProgressCtx(ctx, src, dst, progress); err != nil {
			return err
		}
	}
	return nil
}

// MkdirAll creates a remote directory and any missing parents.
func (c *SFTPClient) MkdirAll(remotePath string) error {
	remotePath = path.Clean(remotePath)
	if remotePath == "." || remotePath == "/" {
		return nil
	}
	parts := strings.Split(remotePath, "/")
	cur := ""
	if strings.HasPrefix(remotePath, "/") {
		cur = "/"
	}
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if cur == "/" {
			cur = "/" + p
		} else if cur == "" {
			cur = p
		} else {
			cur = path.Join(cur, p)
		}
		info, err := c.Stat(cur)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s exists and is not a directory", cur)
			}
			continue
		}
		if err := c.Mkdir(cur); err != nil {
			// Race or already exists — re-stat
			if info, err2 := c.Stat(cur); err2 == nil && info.IsDir() {
				continue
			}
			return fmt.Errorf("mkdir %s: %w", cur, err)
		}
	}
	return nil
}
