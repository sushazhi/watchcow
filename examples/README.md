# WatchCow Application Examples

这个目录包含了各种应用的示例配置，展示如何让 Docker 容器被 WatchCow 自动发现并注册到 fnOS。

## 可用示例

| 示例 | 说明 | 端口 | 文档 |
|------|------|------|------|
| [nginx](./nginx/) | Nginx Web 服务器 | 8080 | [README](./nginx/README.md) |

## 核心概念

### WatchCow Labels

WatchCow 通过读取 Docker 容器的 `labels` 来自动发现应用：

```yaml
services:
  my-app:
    image: my-image
    labels:
      watchcow.enable: "true"           # 启用 WatchCow 发现
      watchcow.appName: "my-app"        # 应用名称
      watchcow.title: "我的应用"         # 显示标题
      watchcow.port: "8080"             # 外部端口
      watchcow.fnDomain: "my-app"       # fnOS 域名
```

### 配置文件结构

每个示例包含标准的三个配置文件：

```
app-name/
├── compose.yaml     # Docker Compose 配置（包含 labels）
├── default.env      # 默认配置（不要修改，required: true）
└── override.env     # 用户自定义配置（可选，required: false）
```

**好处**：
- ✅ **default.env** 保持原始默认值，可安全更新（必需文件）
- ✅ **override.env** 用户自定义，不会被覆盖（可选文件）
- ✅ 配置优先级：`override.env` > `default.env`
- ✅ 不需要自定义时，可以不创建 `override.env`

**env_file 配置**：
```yaml
env_file:
  - path: ./default.env
    required: true      # 必须存在
  - path: ./override.env
    required: false     # 可选，不存在也不报错
```

## 快速开始

### 1. 启动 WatchCow

```bash
# 在项目根目录
docker-compose up -d
```

### 2. 启动示例应用

```bash
# 启动 nginx 示例
cd examples/nginx
docker-compose up -d
```

### 3. 验证

WatchCow 会自动：
- 🔍 发现新启动的容器
- 📝 读取容器的 labels
- 📡 向 fnOS 注册应用
- 👀 监控容器状态（启动/停止）

查看 WatchCow 日志：
```bash
docker logs -f watchcow
```

应该看到类似输出：
```
▶️  Container started: nginx-example
✅ Registered app: nginx.example
```

## 创建自己的应用

### 方法 1: 复制模板

```bash
# 1. 复制 nginx 示例作为模板
cp -r examples/nginx examples/my-app

# 2. 编辑 default.env
cd examples/my-app
vi default.env

# 修改这些值：
# APP_NAME=my-app
# APP_TITLE=我的应用
# APP_PORT=8080
# APP_FN_DOMAIN=my-app

# 3. 修改 compose.yaml
vi compose.yaml

# 修改镜像：
# image: your-image:tag

# 4. 启动
docker-compose up -d
```

### 方法 2: 在现有项目中添加

在你的 `docker-compose.yml` 中添加 labels：

```yaml
version: '3.8'

services:
  your-app:
    image: your-image
    ports:
      - "8080:80"

    # 添加 WatchCow labels
    labels:
      watchcow.enable: "true"
      watchcow.appName: "your-app"
      watchcow.title: "你的应用"
      watchcow.desc: "应用描述"
      watchcow.icon: "https://example.com/icon.png"
      watchcow.port: "8080"
      watchcow.fnDomain: "your-app"
      watchcow.category: "工具"
```

## Label 参考

### 必需 Labels

| Label | 类型 | 说明 | 示例 |
|-------|------|------|------|
| `watchcow.enable` | string | 启用发现 | `"true"` |
| `watchcow.appName` | string | 应用唯一标识 | `"nginx.example"` |
| `watchcow.title` | string | 显示标题 | `"我的应用"` |
| `watchcow.port` | string | 外部端口 | `"8080"` |
| `watchcow.fnDomain` | string | fnOS 域名 | `"my-app"` |

### 可选 Labels

| Label | 类型 | 默认值 | 说明 |
|-------|------|--------|------|
| `watchcow.appID` | string | 自动生成 | 应用 ID |
| `watchcow.entryName` | string | = appName | Entry 名称 |
| `watchcow.desc` | string | - | 描述 |
| `watchcow.icon` | string | Docker 图标 | 图标 URL |
| `watchcow.category` | string | `"其他"` | 分类 |
| `watchcow.protocol` | string | `"http"` | 协议 (http/https) |
| `watchcow.path` | string | `"/"` | URL 路径 |
| `watchcow.microApp` | string | `"false"` | 是否为微应用 |
| `watchcow.nativeApp` | string | `"false"` | 是否为原生应用 |
| `watchcow.isDisplay` | string | `"true"` | 是否显示 |

## 环境变量

Labels 支持使用环境变量：

```yaml
labels:
  watchcow.port: "${APP_PORT}"
  watchcow.title: "${APP_TITLE}"
```

配合 `.env` 文件使用：

```env
APP_PORT=8080
APP_TITLE=我的应用
```

## 图标资源

推荐使用这些免费图标库：

- [Dashboard Icons](https://github.com/walkxcode/dashboard-icons) - 常见应用图标
- [Simple Icons](https://simpleicons.org/) - 品牌图标
- [Pictogrammers](https://pictogrammers.com/) - Material Design 图标

示例：
```yaml
watchcow.icon: "https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/nginx.png"
```

## 常见应用分类

建议使用以下分类名称：

- 📦 **开发工具**: VSCode, GitLab, Jenkins
- 🌐 **网络服务**: Nginx, Traefik, Caddy
- 💾 **数据库**: MySQL, PostgreSQL, MongoDB
- 📊 **监控运维**: Grafana, Prometheus, Uptime Kuma
- 📁 **文件管理**: Nextcloud, FileBrowser, Syncthing
- 🎬 **媒体影音**: Jellyfin, Plex, Emby
- 📝 **笔记文档**: Notion, Obsidian, Wiki.js
- 🛠️ **系统工具**: Portainer, Watchtower, Dozzle
- 🏠 **智能家居**: Home Assistant, Node-RED
- 📧 **通讯协作**: Mattermost, Rocket.Chat
- 🎨 **设计创作**: Draw.io, Excalidraw
- 🔐 **安全加密**: Vaultwarden, Authelia
- 📚 **学习教育**: Calibre-Web, BookStack
- 🎮 **游戏娱乐**: Minecraft, Palworld
- 🤖 **AI 工具**: Ollama, Stable Diffusion
- 📦 **其他**: 未分类应用

## 故障排查

### 应用未被发现

1. 检查 WatchCow 是否运行：
   ```bash
   docker ps | grep watchcow
   ```

2. 检查容器 labels：
   ```bash
   docker inspect your-container | grep watchcow
   ```

3. 确认 `watchcow.enable` 设置为 `"true"`

### 应用信息不正确

1. 修改 `override.env` 或 compose.yaml 中的 labels
2. 重启容器：
   ```bash
   docker-compose restart
   ```

### 端口冲突

修改 `override.env`：
```env
APP_PORT=9090
```

## 贡献示例

欢迎提交新的应用示例！

1. Fork 项目
2. 复制 `examples/nginx` 作为模板
3. 修改配置文件
4. 测试验证
5. 提交 Pull Request

### 示例要求

- ✅ 包含完整的 compose.yaml, default.env, override.env
- ✅ 提供详细的 README.md
- ✅ 使用官方或可信的 Docker 镜像
- ✅ 配置合理的健康检查
- ✅ 默认端口不冲突（8080+）
- ✅ 测试验证可正常运行

## 相关资源

- [WatchCow 项目主页](https://github.com/xiaxilin/watchcow)
- [fnOS 官方文档](https://docs.fnnas.com)
- [Docker Compose 文档](https://docs.docker.com/compose/)
- [Docker Labels 最佳实践](https://docs.docker.com/config/labels-custom-metadata/)

## 许可证

MIT License
