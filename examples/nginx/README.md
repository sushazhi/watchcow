# Nginx Example - WatchCow Application Template

这是一个标准的 WatchCow 应用示例，展示如何让 Docker 容器被 WatchCow 自动发现并注册到 fnOS。

## 目录结构

```
nginx/
├── compose.yaml     # Docker Compose 配置（包含默认值）
├── .env.example     # 配置模板（复制为 .env 自定义）
├── html/            # 网站文件目录（可选）
│   └── index.html
├── README.md        # 说明文档
└── TEST.md          # 测试指南
```

## 快速开始

### 1. 基础使用（零配置）

```bash
cd examples/nginx
docker-compose up -d
```

访问: http://localhost:8080

**✨ 无需任何配置文件，直接启动即可！**

### 2. 自定义配置

如果需要自定义，创建 `.env` 文件：

```bash
# 方法 1: 从模板复制
cp .env.example .env
vi .env

# 方法 2: 直接创建
cat > .env << 'EOF'
APP_PORT=9090
APP_TITLE=我的 Nginx
APP_CATEGORY=Web 服务
EOF
```

然后重启：

```bash
docker-compose restart
```

### 3. 验证 WatchCow 标签

查看容器的 labels：

```bash
docker inspect nginx-example | grep watchcow
```

应该看到类似输出：

```json
"watchcow.enable": "true",
"watchcow.appName": "nginx.example",
"watchcow.title": "Nginx 示例",
"watchcow.port": "8080",
...
```

## WatchCow Labels 说明

### 必需标签

| Label | 说明 | 默认值 |
|-------|------|--------|
| `watchcow.enable` | 启用发现 | `"true"` |
| `watchcow.appName` | 应用唯一标识 | `"nginx.example"` |
| `watchcow.title` | 显示标题 | `"Nginx 示例"` |
| `watchcow.port` | 外部端口 | `"8080"` |
| `watchcow.fnDomain` | fnOS 域名 | `"nginx-example"` |

### 可选标签

| Label | 说明 | 默认值 |
|-------|------|--------|
| `watchcow.appID` | 应用 ID | `"nginx-001"` |
| `watchcow.desc` | 应用描述 | `"Nginx Web 服务器示例应用"` |
| `watchcow.icon` | 图标 URL | nginx 图标 URL |
| `watchcow.category` | 分类 | `"开发工具"` |
| `watchcow.protocol` | 协议 | `"http"` |
| `watchcow.path` | URL 路径 | `"/"` |
| `watchcow.microApp` | 是否为微应用 | `"false"` |
| `watchcow.nativeApp` | 是否为原生应用 | `"false"` |
| `watchcow.isDisplay` | 是否显示 | `"true"` |

## 配置文件说明

### compose.yaml

包含所有默认配置，使用 `${VAR:-default}` 语法：

```yaml
watchcow.title: "${APP_TITLE:-Nginx 示例}"  # 默认：Nginx 示例
watchcow.port: "${APP_PORT:-8080}"          # 默认：8080
```

**特点**：
- ✅ 默认值硬编码，无需配置文件即可启动
- ✅ 提交到 Git，作为标准配置
- ✅ 通过环境变量覆盖

### .env（可选）

用户自定义配置文件，用于覆盖默认值：

```env
# 只需要包含你想修改的配置
APP_PORT=9090
APP_TITLE=我的应用
```

**特点**：
- 📝 可选文件，不存在不会报错
- 🚫 已添加到 .gitignore，不会提交
- ⚙️ 只包含需要修改的配置项

### .env.example

配置模板文件，包含所有可用变量：

```bash
# 复制模板快速开始
cp .env.example .env
```

**特点**：
- ✅ 提交到 Git，作为参考
- 📖 包含所有配置项的注释
- 🚀 新用户可快速上手

## 创建自己的应用

### 方法 1: 复制此示例

```bash
# 1. 复制示例目录
cp -r examples/nginx examples/my-app

# 2. 修改 compose.yaml 中的默认值
cd examples/my-app
vi compose.yaml
# 修改 labels 中的 ${VAR:-default} 默认值

# 3. 修改镜像
# 将 nginx:alpine 改为你的镜像

# 4. 启动（零配置）
docker-compose up -d

# 5. 可选：创建 .env 自定义
cp .env.example .env
vi .env
docker-compose restart
```

### 方法 2: 在现有 Compose 文件中添加 labels

```yaml
services:
  your-app:
    image: your-image
    labels:
      watchcow.enable: "true"
      watchcow.appName: "${APP_NAME:-your-app}"
      watchcow.title: "${APP_TITLE:-你的应用}"
      watchcow.port: "${APP_PORT:-8080}"
      watchcow.fnDomain: "${APP_FN_DOMAIN:-your-app}"
```

## 与 WatchCow 集成

### 1. 启动 WatchCow

```bash
cd /path/to/watchcow
docker-compose up -d
```

### 2. 启动你的应用

```bash
cd examples/nginx
docker-compose up -d
```

### 3. WatchCow 会自动：

- ✅ 发现带有 `watchcow.enable=true` 的容器
- ✅ 读取容器的 labels
- ✅ 向 fnOS 注册应用
- ✅ 监控容器状态变化（启动/停止）

## 环境变量优先级

```
.env 文件  >  ${VAR:-default} 中的 default 值
```

示例：

1. `compose.yaml`: `watchcow.port: "${APP_PORT:-8080}"`
2. 不创建 `.env`: 使用默认值 `8080`
3. 创建 `.env` 设置 `APP_PORT=9090`: 使用 `9090`

## 常见问题

### Q: 为什么没有 default.env 和 override.env？

**A:** 新设计更简单：
- ✅ 默认值直接在 `compose.yaml` 中定义
- ✅ 自定义时创建 `.env` 覆盖
- ✅ 无需维护两个配置文件
- ✅ 零配置即可启动

### Q: 如何让应用不在 fnOS 显示？

**A:** 创建 `.env` 文件：
```env
APP_IS_DISPLAY=false
```

### Q: 如何使用 HTTPS？

**A:** 在 `.env` 中配置：
```env
APP_PROTOCOL=https
APP_PORT=443
```

### Q: WatchCow 何时会重新读取 labels？

**A:** 当容器重启时，WatchCow 会自动检测并更新应用信息。

## 自定义示例

### 示例 1: 修改端口

```bash
# .env
APP_PORT=9090
```

### 示例 2: 完全自定义

```bash
# .env
APP_NAME=my-nginx
APP_TITLE=我的 Web 服务器
APP_DESC=个人网站服务器
APP_PORT=8888
APP_CATEGORY=个人项目
APP_ICON=https://example.com/icon.png
```

### 示例 3: 隐藏应用

```bash
# .env
APP_IS_DISPLAY=false
```

## 参考资料

- [fnOS 应用规范](https://docs.fnnas.com)
- [Docker Compose 文档](https://docs.docker.com/compose/)
- [Docker Compose 环境变量](https://docs.docker.com/compose/environment-variables/)
- [WatchCow 项目主页](https://github.com/xiaxilin/watchcow)
