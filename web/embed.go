package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var distFS embed.FS

// GetDistFS 返回前端构建产物的文件系统
func GetDistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
