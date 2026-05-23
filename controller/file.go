package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"webssh/core"

	"github.com/gin-gonic/gin"
	"github.com/pkg/sftp"
)

// File 结构体
type File struct {
	Name       string
	Size       string
	ModifyTime string
	IsDir      bool
}

const (
	// BYTE 字节
	BYTE = 1 << (10 * iota)
	// KILOBYTE 千字节
	KILOBYTE
	// MEGABYTE 兆字节
	MEGABYTE
	// GIGABYTE 吉字节
	GIGABYTE
	// TERABYTE 太字节
	TERABYTE
	// PETABYTE 拍字节
	PETABYTE
	// EXABYTE 艾字节
	EXABYTE
)

// Bytefmt returns a human-readable byte string of the form 10M, 12.5K, and so forth.  The following units are available:
//	E: Exabyte
//	P: Petabyte
//	T: Terabyte
//	G: Gigabyte
//	M: Megabyte
//	K: Kilobyte
//	B: Byte
// The unit that results in the smallest number greater than or equal to 1 is always chosen.
func Bytefmt(bytes uint64) string {
	unit := ""
	value := float64(bytes)

	switch {
	case bytes >= EXABYTE:
		unit = "E"
		value = value / EXABYTE
	case bytes >= PETABYTE:
		unit = "P"
		value = value / PETABYTE
	case bytes >= TERABYTE:
		unit = "T"
		value = value / TERABYTE
	case bytes >= GIGABYTE:
		unit = "G"
		value = value / GIGABYTE
	case bytes >= MEGABYTE:
		unit = "M"
		value = value / MEGABYTE
	case bytes >= KILOBYTE:
		unit = "K"
		value = value / KILOBYTE
	case bytes >= BYTE:
		unit = "B"
	case bytes == 0:
		return "0B"
	}

	result := strconv.FormatFloat(value, 'f', 2, 64)
	result = strings.TrimSuffix(result, ".00")
	return result + unit
}

type fileSplice []File

// Len 比较大小
func (f fileSplice) Len() int { return len(f) }

// Swap 交换
func (f fileSplice) Swap(i, j int) { f[i], f[j] = f[j], f[i] }

// Less 比大小
func (f fileSplice) Less(i, j int) bool { return f[i].IsDir }

// UploadFile 上传文件
func UploadFile(c *gin.Context) *ResponseBody {
	var (
		sshClient core.SSHClient
		err       error
	)
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	sshInfo := c.PostForm("sshInfo")
	id := c.PostForm("id")
	if sshClient, err = core.DecodedMsgToSSHClient(sshInfo); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	if err := sshClient.CreateSftp(); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer sshClient.Close()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer file.Close()
	path := strings.TrimSpace(c.DefaultPostForm("path", ""))
	if path == "" {
		path = detectHomeDir(sshClient.Sftp, sshClient.Username)
	}
	pathArr := []string{strings.TrimRight(path, "/")}
	if dir := c.DefaultPostForm("dir", ""); "" != dir {
		pathArr = append(pathArr, dir)
		if err := sshClient.Mkdirs(strings.Join(pathArr, "/")); err != nil {
			responseBody.Msg = err.Error()
			return &responseBody
		}
	}
	pathArr = append(pathArr, header.Filename)
	err = sshClient.Upload(file, id, strings.Join(pathArr, "/"))
	if err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
	}
	return &responseBody
}

// DownloadFile 下载文件
func DownloadFile(c *gin.Context) *ResponseBody {
	var (
		sshClient core.SSHClient
		err       error
	)
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	path := strings.TrimSpace(c.DefaultQuery("path", ""))
	if path == "" {
		path = detectHomeDir(sshClient.Sftp, sshClient.Username)
	}
	sshInfo := c.DefaultQuery("sshInfo", "")
	if sshClient, err = core.DecodedMsgToSSHClient(sshInfo); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	if err := sshClient.CreateSftp(); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer sshClient.Close()
	if sftpFile, err := sshClient.Download(path); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
	} else {
		defer sftpFile.Close()
		c.Writer.WriteHeader(http.StatusOK)
		fileMeta := strings.Split(path, "/")
		c.Header("Content-Disposition", "attachment; filename="+fileMeta[len(fileMeta)-1])
		_, _ = io.Copy(c.Writer, sftpFile)
	}
	return &responseBody
}

// UploadProgressWs 获取上传进度ws
func UploadProgressWs(c *gin.Context) *ResponseBody {
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	id := c.Query("id")

	var ready, find bool
	for {
		if !ready && core.WcList == nil {
			continue
		}
		for _, v := range core.WcList {
			if v.Id == id {
				wsConn.WriteMessage(1, []byte(strconv.Itoa(v.Total)))
				find = true
				if !ready {
					ready = true
				}
				break
			}
		}
		if ready && !find {
			wsConn.Close()
			break
		}

		if ready {
			time.Sleep(300 * time.Millisecond)
			find = false
		}
	}
	return &responseBody
}

// FileList 获取文件列表
func FileList(c *gin.Context) *ResponseBody {
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	path := c.DefaultQuery("path", "")
	sshInfo := c.DefaultQuery("sshInfo", "")
	sshClient, err := core.DecodedMsgToSSHClient(sshInfo)
	if err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	if err := sshClient.CreateSftp(); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer sshClient.Close()

	// 检测 home 目录
	home := detectHomeDir(sshClient.Sftp, sshClient.Username)

	// 如果 path 为 / 且 home 不为 /，且不是 root 用户，自动切换到 home
	if path == "/" && home != "/" && sshClient.Username != "root" {
		path = home
	}

	// 1. 如果 path 为空，首次进入，普通用户进入 home，root 进入 /
	if path == "" {
		if sshClient.Username == "root" {
			path = "/"
		} else {
			path = home
		}
	}

	files, err := sshClient.Sftp.ReadDir(path)
	if err != nil {
		if strings.Contains(err.Error(), "exist") {
			responseBody.Msg = fmt.Sprintf("Directory %s: no such file or directory", path)
		} else {
			responseBody.Msg = err.Error()
		}
		return &responseBody
	}
	var (
		fileList fileSplice
		fileSize string
	)
	for _, mFile := range files {
		if mFile.IsDir() {
			fileSize = strconv.FormatInt(mFile.Size(), 10)
		} else {
			fileSize = Bytefmt(uint64(mFile.Size()))
		}
		file := File{Name: mFile.Name(), IsDir: mFile.IsDir(), Size: fileSize, ModifyTime: mFile.ModTime().Format("2006-01-02 15:04:05")}
		fileList = append(fileList, file)
	}
	sort.Stable(fileList)
	responseBody.Data = gin.H{
		"list": fileList,
		"home": home, // home 字段始终返回 home 目录
	}
	return &responseBody
}

// 自动检测home目录
func detectHomeDir(sftpClient *sftp.Client, username string) string {
	// 1. 尝试获取当前工作目录
	if wd, err := sftpClient.Getwd(); err == nil && wd != "" {
		return wd
	}

	// 2. 如果是 root 用户，直接返回 /root
	if username == "root" {
		return "/root"
	}

	// 3. 先检测 /usr/home/用户名，再检测 /home/用户名
	potentialHome := fmt.Sprintf("/usr/home/%s", username)
	if _, err := sftpClient.Stat(potentialHome); err == nil {
		return potentialHome
	}
	potentialHome = fmt.Sprintf("/home/%s", username)
	if _, err := sftpClient.Stat(potentialHome); err == nil {
		return potentialHome
	}

	// 4. 如果都失败了，返回根目录
	return "/home"
}

// isTextFile 根据文件扩展名判断是否为文本文件
func isTextFile(filename string) bool {
	// 常见文本文件扩展名
	textExtensions := []string{
		".txt", ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rb", ".php", ".java", ".c", ".cpp", ".h", ".hpp",
		".cs", ".swift", ".kt", ".rs", ".r", ".m", ".mm", ".pl", ".pm", ".sh", ".bash", ".zsh", ".fish",
		".bat", ".cmd", ".ps1", ".vbs", ".lua", ".groovy", ".scala", ".clj", ".cljs", ".cljc", ".edn",
		".json", ".json5", ".jsonc", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".config",
		".xml", ".html", ".htm", ".xhtml", ".svg", ".css", ".scss", ".sass", ".less", ".styl",
		".md", ".markdown", ".rst", ".txt", ".log", ".csv", ".tsv", ".sql", ".graphql", ".gql",
		".dockerfile", ".dockerignore", ".gitignore", ".env", ".editorconfig", ".eslintrc", ".prettierrc",
		".babelrc", ".stylelintrc", ".huskyrc", ".lintstagedrc", ".npmrc", ".yarnrc", ".nvmrc",
		".vue", ".svelte", ".astro", ".ejs", ".hbs", ".handlebars", ".pug", ".jade",
		".proto", ".thrift", ".avsc", ".avdl", ".gql", ".graphql",
	}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, textExt := range textExtensions {
		if ext == textExt {
			return true
		}
	}
	// 如果没有扩展名，检查文件名是否以点开头（如 .gitignore）
	if strings.HasPrefix(filename, ".") && ext == "" {
		return true
	}
	return false
}

// ReadFile 读取文本文件内容
func ReadFile(c *gin.Context) *ResponseBody {
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	path := strings.TrimSpace(c.DefaultQuery("path", ""))
	if path == "" {
		responseBody.Msg = "文件路径不能为空"
		return &responseBody
	}
	sshInfo := c.DefaultQuery("sshInfo", "")
	sshClient, err := core.DecodedMsgToSSHClient(sshInfo)
	if err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	if err := sshClient.CreateSftp(); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer sshClient.Close()

	// 检查文件是否存在
	fileInfo, err := sshClient.Sftp.Stat(path)
	if err != nil {
		responseBody.Msg = "文件不存在或无法访问: " + err.Error()
		return &responseBody
	}
	// 检查是否为目录
	if fileInfo.IsDir() {
		responseBody.Msg = "不能编辑目录"
		return &responseBody
	}
	// 检查文件大小（限制为1MB）
	if fileInfo.Size() > 1*1024*1024 {
		responseBody.Msg = "文件过大，只能编辑小于1MB的文件"
		return &responseBody
	}
	// 检查是否为文本文件
	if !isTextFile(fileInfo.Name()) {
		responseBody.Msg = "不支持编辑此文件类型"
		return &responseBody
	}

	// 打开文件
	sftpFile, err := sshClient.Sftp.Open(path)
	if err != nil {
		responseBody.Msg = "无法打开文件: " + err.Error()
		return &responseBody
	}
	defer sftpFile.Close()

	// 读取文件内容
	content, err := io.ReadAll(sftpFile)
	if err != nil {
		responseBody.Msg = "读取文件失败: " + err.Error()
		return &responseBody
	}

	// 检查是否为二进制文件（包含空字节）
	if bytes.Contains(content, []byte{0}) {
		responseBody.Msg = "不能编辑二进制文件"
		return &responseBody
	}

	responseBody.Data = gin.H{
		"content": string(content),
		"path":    path,
	}
	return &responseBody
}

// SaveFile 保存文本文件内容
func SaveFile(c *gin.Context) *ResponseBody {
	responseBody := ResponseBody{Msg: "success"}
	defer TimeCost(time.Now(), &responseBody)
	var requestData struct {
		SSHInfo string `json:"sshInfo"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&requestData); err != nil {
		responseBody.Msg = "请求参数错误: " + err.Error()
		return &responseBody
	}
	if requestData.Path == "" {
		responseBody.Msg = "文件路径不能为空"
		return &responseBody
	}
	sshClient, err := core.DecodedMsgToSSHClient(requestData.SSHInfo)
	if err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	if err := sshClient.CreateSftp(); err != nil {
		fmt.Println(err)
		responseBody.Msg = err.Error()
		return &responseBody
	}
	defer sshClient.Close()

	// 检查文件是否存在
	fileInfo, err := sshClient.Sftp.Stat(requestData.Path)
	if err != nil {
		responseBody.Msg = "文件不存在或无法访问: " + err.Error()
		return &responseBody
	}
	// 检查是否为目录
	if fileInfo.IsDir() {
		responseBody.Msg = "不能保存到目录"
		return &responseBody
	}
	// 检查是否为文本文件
	if !isTextFile(fileInfo.Name()) {
		responseBody.Msg = "不支持编辑此文件类型"
		return &responseBody
	}

	// 打开文件进行写入（截断模式）
	sftpFile, err := sshClient.Sftp.OpenFile(requestData.Path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE)
	if err != nil {
		responseBody.Msg = "无法打开文件进行写入: " + err.Error()
		return &responseBody
	}
	defer sftpFile.Close()

	// 写入内容
	_, err = sftpFile.Write([]byte(requestData.Content))
	if err != nil {
		responseBody.Msg = "写入文件失败: " + err.Error()
		return &responseBody
	}

	return &responseBody
}
