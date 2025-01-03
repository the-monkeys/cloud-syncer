package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const inputFolderMapPath = "dir_map.json"
const credFilePath = "service-account.json"

func main() {
	localServerDirMap := map[string]string{}

	bx, err := os.ReadFile(inputFolderMapPath)
	if err != nil {
		fmt.Printf("Error reading input folder map: %v\n", err)
		os.Exit(1)
	}

	if err := json.Unmarshal(bx, &localServerDirMap); err != nil {
		fmt.Printf("Error unmarshaling input folder map: %v\n", err)
		os.Exit(1)
	}

	srv, err := getDriveService()
	if err != nil {
		fmt.Printf("Error creating Drive service: %v\n", err)
		os.Exit(1)
	}

	var wg sync.WaitGroup

	for localDir, remoteFolderID := range localServerDirMap {
		wg.Add(1)
		go func(localDir, remoteFolderID string) {
			defer wg.Done()
			fmt.Printf("Syncing %s with folder ID %s\n", localDir, remoteFolderID)
			if err := syncFiles(localDir, remoteFolderID, srv); err != nil {
				fmt.Printf("Error syncing files for %s: %v\n", localDir, err)
			}
		}(localDir, remoteFolderID)
	}

	wg.Wait()
	fmt.Println("Sync completed successfully!")
}

func getDriveService() (*drive.Service, error) {
	ctx := context.Background()
	srv, err := drive.NewService(ctx, option.WithCredentialsFile(credFilePath))
	if err != nil {
		return nil, fmt.Errorf("unable to create Drive service: %v", err)
	}
	return srv, nil
}

func syncFiles(localDir, remoteFolderID string, srv *drive.Service) error {
	remoteFiles := make(map[string]*drive.File)
	folderCache := make(map[string]string) // Cache for created folders
	if err := fetchRemoteFiles(remoteFolderID, "", remoteFiles, srv); err != nil {
		return fmt.Errorf("failed to fetch remote files: %v", err)
	}

	localFiles := make(map[string]string)
	if err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(localDir, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %v", err)
		}

		checksum, err := computeMD5(path)
		if err != nil {
			return fmt.Errorf("failed to compute checksum for %s: %v", path, err)
		}

		localFiles[relativePath] = checksum
		return uploadOrUpdateFile(path, relativePath, checksum, remoteFiles, remoteFolderID, folderCache, srv)
	}); err != nil {
		return err
	}

	if err := deleteRemoteFiles(localFiles, remoteFiles, srv); err != nil {
		return fmt.Errorf("failed to delete remote files: %v", err)
	}

	return nil
}

func fetchRemoteFiles(parentID, path string, remoteFiles map[string]*drive.File, srv *drive.Service) error {
	pageToken := ""
	for {
		query := fmt.Sprintf("'%s' in parents and trashed=false", parentID)
		result, err := srv.Files.List().Q(query).Fields("nextPageToken, files(id, name, md5Checksum, mimeType)").PageToken(pageToken).Do()
		if err != nil {
			return fmt.Errorf("unable to list files: %v", err)
		}

		for _, file := range result.Files {
			remotePath := filepath.Join(path, file.Name)
			if file.MimeType == "application/vnd.google-apps.folder" {
				if err := fetchRemoteFiles(file.Id, remotePath, remoteFiles, srv); err != nil {
					return err
				}
			} else {
				remoteFiles[remotePath] = file
			}
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return nil
}

func uploadOrUpdateFile(filePath, relativePath, checksum string, remoteFiles map[string]*drive.File, parentID string, folderCache map[string]string, srv *drive.Service) error {
	remoteFile, exists := remoteFiles[relativePath]
	if exists {
		if remoteFile.Md5Checksum == checksum {
			fmt.Printf("File already exists and is identical: %s\n", relativePath)
			return nil
		}
		fmt.Printf("Updating file: %s\n", relativePath)
		return updateFile(filePath, remoteFile.Id, srv)
	}

	fmt.Printf("Uploading new file: %s\n", relativePath)
	return uploadFile(filePath, relativePath, parentID, folderCache, srv)
}

func deleteRemoteFiles(localFiles map[string]string, remoteFiles map[string]*drive.File, srv *drive.Service) error {
	for remotePath, remoteFile := range remoteFiles {
		if _, exists := localFiles[remotePath]; !exists {
			fmt.Printf("Deleting remote file: %s\n", remotePath)
			if err := srv.Files.Delete(remoteFile.Id).Do(); err != nil {
				return fmt.Errorf("failed to delete file %s: %v", remotePath, err)
			}
		}
	}
	return nil
}

func computeMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("unable to open file: %v", err)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("unable to compute hash: %v", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func updateFile(filePath, fileID string, srv *drive.Service) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open file: %v", err)
	}
	defer file.Close()

	_, err = srv.Files.Update(fileID, nil).Media(file).Do()
	if err != nil {
		return fmt.Errorf("unable to update file: %v", err)
	}

	fmt.Printf("Updated: %s\n", filePath)
	return nil
}

func uploadFile(filePath, relativePath, parentID string, folderCache map[string]string, srv *drive.Service) error {
	drivePathParts := strings.Split(filepath.ToSlash(relativePath), "/")
	var err error

	for i, part := range drivePathParts[:len(drivePathParts)-1] {
		cacheKey := strings.Join(drivePathParts[:i+1], "/")
		if cachedID, found := folderCache[cacheKey]; found {
			parentID = cachedID
			continue
		}

		parentID, err = createOrGetFolder(part, parentID, srv)
		if err != nil {
			return fmt.Errorf("unable to create or get folder '%s': %v", cacheKey, err)
		}
		folderCache[cacheKey] = parentID
	}

	fileName := drivePathParts[len(drivePathParts)-1]
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open file: %v", err)
	}
	defer file.Close()

	driveFile := &drive.File{
		Name:    fileName,
		Parents: []string{parentID},
	}

	_, err = srv.Files.Create(driveFile).Media(file).Do()
	if err != nil {
		return fmt.Errorf("unable to upload file: %v", err)
	}

	fmt.Printf("Uploaded: %s\n", filePath)
	return nil
}

func createOrGetFolder(folderName, parentID string, srv *drive.Service) (string, error) {
	query := fmt.Sprintf("name='%s' and '%s' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false", folderName, parentID)
	result, err := srv.Files.List().Q(query).Fields("files(id)").Do()
	if err != nil {
		return "", fmt.Errorf("unable to query folders: %v", err)
	}

	if len(result.Files) > 0 {
		return result.Files[0].Id, nil
	}

	// Folder doesn't exist, create it
	folder := &drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}

	createdFolder, err := srv.Files.Create(folder).Do()
	if err != nil {
		return "", fmt.Errorf("unable to create folder: %v", err)
	}

	return createdFolder.Id, nil
}
