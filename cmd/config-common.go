package cmd

import (
	"bytes"
	"context"
	"errors"

	"github.com/minio/radio/cmd/logger"
	"github.com/minio/minio/pkg/hash"
)

var errConfigNotFound = errors.New("config file not found")

func readConfig(ctx context.Context, objAPI ObjectLayer, configFile string) ([]byte, error) {
	var buffer bytes.Buffer
	// Read entire content by setting size to -1
	if err := objAPI.GetObject(ctx, minioMetaBucket, configFile, 0, -1, &buffer, "", ObjectOptions{}); err != nil {
		// Treat object not found as config not found.
		if isErrObjectNotFound(err) {
			return nil, errConfigNotFound
		}

		logger.GetReqInfo(ctx).AppendTags("configFile", configFile)
		logger.LogIf(ctx, err)
		return nil, err
	}

	// Return config not found on empty content.
	if buffer.Len() == 0 {
		return nil, errConfigNotFound
	}

	return buffer.Bytes(), nil
}

func deleteConfig(ctx context.Context, objAPI ObjectLayer, configFile string) error {
	return objAPI.DeleteObject(ctx, minioMetaBucket, configFile)
}

func saveConfig(ctx context.Context, objAPI ObjectLayer, configFile string, data []byte) error {
	hashReader, err := hash.NewReader(bytes.NewReader(data), int64(len(data)), "", getSHA256Hash(data), int64(len(data)), globalCLIContext.StrictS3Compat)
	if err != nil {
		return err
	}

	_, err = objAPI.PutObject(ctx, minioMetaBucket, configFile, NewPutObjReader(hashReader, nil, nil), ObjectOptions{})
	return err
}

func checkConfig(ctx context.Context, objAPI ObjectLayer, configFile string) error {
	if _, err := objAPI.GetObjectInfo(ctx, minioMetaBucket, configFile, ObjectOptions{}); err != nil {
		// Treat object not found as config not found.
		if isErrObjectNotFound(err) {
			return errConfigNotFound
		}

		logger.GetReqInfo(ctx).AppendTags("configFile", configFile)
		logger.LogIf(ctx, err)
		return err
	}
	return nil
}
