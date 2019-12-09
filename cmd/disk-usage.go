package cmd

import (
	"context"
)

// getDiskUsage walks the file tree rooted at root, calling usageFn
// for each file or directory in the tree, including root.
func getDiskUsage(ctx context.Context, root string, usageFn usageFunc) error {
	return walk(ctx, root+SlashSeparator, usageFn)
}

type usageFunc func(ctx context.Context, entry string) error

// walk recursively descends path, calling walkFn.
func walk(ctx context.Context, path string, usageFn usageFunc) error {
	if err := usageFn(ctx, path); err != nil {
		return err
	}

	if !HasSuffix(path, SlashSeparator) {
		return nil
	}

	entries, err := readDir(path)
	if err != nil {
		return usageFn(ctx, path)
	}

	for _, entry := range entries {
		fname := pathJoin(path, entry)
		if err = walk(ctx, fname, usageFn); err != nil {
			return err
		}
	}

	return nil
}
