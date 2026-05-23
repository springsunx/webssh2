import request from '@/utils/request'
export function fileList(path, sshInfo) {
    return request.get(`/file/list?path=${path}&sshInfo=${sshInfo}`)
}

export function readFile(path, sshInfo) {
    return request.get(`/file/read?path=${path}&sshInfo=${sshInfo}`)
}

export function saveFile(path, content, sshInfo) {
    return request.post('/file/save', { path, content, sshInfo })
}
