/* ============================================
   AutoTakeGo - API Client
   ============================================ */

const API = {
  baseURL: '/api/v1',
  token: localStorage.getItem('token') || '',

  async request(method, path, body, isFormData = false) {
    const headers = { 'Authorization': `Bearer ${this.token}` }
    if (!isFormData) headers['Content-Type'] = 'application/json'
    
    const res = await fetch(`${this.baseURL}${path}`, {
      method,
      headers,
      body: isFormData ? body : (body ? JSON.stringify(body) : undefined)
    })
    
    if (res.status === 401) {
      localStorage.removeItem('token')
      location.href = '/login.html'
      throw new Error('Unauthorized')
    }
    
    const json = await res.json()
    if (json.code !== 0 && json.code !== 200) throw new Error(json.msg || 'Request failed')
    return json.data
  },

  get(path) { return this.request('GET', path) },
  post(path, body) { return this.request('POST', path, body) },
  put(path, body) { return this.request('PUT', path, body) },
  patch(path, body) { return this.request('PATCH', path, body) },
  del(path) { return this.request('DELETE', path) },

  // 任务
  listTasks(params = {}) { return this.get('/tasks?' + new URLSearchParams(params)) },
  getTask(id) { return this.get(`/tasks/${id}`) },
  createTask(data) { return this.post('/tasks', data) },
  updateTask(id, data) { return this.put(`/tasks/${id}`, data) },
  deleteTask(id) { return this.del(`/tasks/${id}`) },
  runTask(id) { return this.post(`/tasks/${id}/run`, {}) },
  stopTask(id) { return this.post(`/tasks/${id}/stop`, {}) },
  cloneTask(id) { return this.post(`/tasks/${id}/clone`, {}) },
  exportTask(id) { return this.get(`/tasks/${id}/export`) },

  // 执行记录
  listExecutions(params = {}) { return this.get('/executions?' + new URLSearchParams(params)) },
  getExecution(id) { return this.get(`/executions/${id}`) },
  getScreenshot(id) { return `${this.baseURL}/executions/${id}/screenshot` },
  getSource(id) { return `${this.baseURL}/executions/${id}/source` },

  // Worker
  listWorkers() { return this.get('/workers') },

  // 模板
  listTemplates() { return this.get('/templates') },
  createTemplate(data) { return this.post('/templates', data) },
  applyTemplate(id) { return this.post(`/templates/${id}/apply`, {}) },
  deleteTemplate(id) { return this.del(`/templates/${id}`) },

  // 导入
  importTask(formData) { return this.request('POST', '/tasks/import', formData, true) },

  // 认证
  login(username, password) {
    return this.post('/auth/login', { username, password })
  }
}

// 设置 token
API.setToken = (token) => {
  API.token = token
  localStorage.setItem('token', token)
}

// 获取当前 token
API.getToken = () => {
  return localStorage.getItem('token') || ''
}

// 登出
API.logout = () => {
  localStorage.removeItem('token')
  API.token = ''
  location.href = '/login.html'
}

// 初始化 token
API.token = API.getToken()
