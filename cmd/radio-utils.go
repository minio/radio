package cmd

// reduceWriteErrors returns error if both remotes returned an error
// Otherwise, rindex would have the index of remote where operation succeeded.
func reduceWriteErrs(errs []error) (rindex int, err error) {
	errCnt := 0
	rindex = -1
	for index, err := range errs {
		if err != nil {
			errCnt++
		} else {
			rindex = index
		}
	}
	if errCnt == len(errs) {
		err = errs[0]
	}
	return
}
