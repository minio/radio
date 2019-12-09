package cmd

import (
	"context"
	"net/http"
	"net/url"
)

// Writes S3 compatible copy part range error.
func writeCopyPartErr(ctx context.Context, w http.ResponseWriter, err error, url *url.URL) {
	switch err {
	case errInvalidRange:
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopyPartRange), url)
		return
	case errInvalidRangeSource:
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopyPartRangeSource), url)
		return
	default:
		writeErrorResponse(ctx, w, toAPIError(ctx, err), url)
		return
	}
}

// Parses x-amz-copy-source-range for CopyObjectPart API. Its behavior
// is different from regular HTTP range header. It only supports the
// form `bytes=first-last` where first and last are zero-based byte
// offsets. See
// http://docs.aws.amazon.com/AmazonS3/latest/API/mpUploadUploadPartCopy.html
// for full details. This function treats an empty rangeString as
// referring to the whole resource.
func parseCopyPartRangeSpec(rangeString string) (hrange *HTTPRangeSpec, err error) {
	hrange, err = parseRequestRangeSpec(rangeString)
	if err != nil {
		return nil, err
	}
	if hrange.IsSuffixLength || hrange.Start < 0 || hrange.End < 0 {
		return nil, errInvalidRange
	}
	return hrange, nil
}

// checkCopyPartRangeWithSize adds more check to the range string in case of
// copy object part. This API requires having specific start and end  range values
// e.g. 'bytes=3-10'. Other use cases will be rejected.
func checkCopyPartRangeWithSize(rs *HTTPRangeSpec, resourceSize int64) (err error) {
	if rs == nil {
		return nil
	}
	if rs.IsSuffixLength || rs.Start >= resourceSize || rs.End >= resourceSize {
		return errInvalidRangeSource
	}
	return nil
}
