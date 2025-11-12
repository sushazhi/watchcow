# Memos Example - WatchCow Application Template

这是一个 Memos 应用的 WatchCow 配置示例，展示如何让 Memos 容器被 WatchCow 自动发现并注册到 fnOS。

## 关于 Memos

Memos 是一个轻量级的、开源的笔记和知识管理工具，支持 Markdown 语法，提供简洁的界面和强大的功能。

- 官网: https://usememos.com
- GitHub: https://github.com/usememos/memos

## 目录结构

```
memos/
├── compose.yaml     # Docker Compose 配置（包含默认值）
├── .env.example     # 配置模板（复制为 .env 自定义）
├── volumes/         # 数据持久化目录（自动创建）
│   └── memos_data/
└── README.md        # 说明文档
```

## 快速开始

### 1. 基础使用（零配置）

```bash
cd examples/memos
docker-compose up -d
```

访问: http://localhost:5230

**✨ 无需任何配置文件，直接启动即可！**

第一次访问需要创建管理员账号。

### 2. 自定义配置

如果需要自定义端口或其他设置，创建 `.env` 文件：

```bash
# 方法 1: 从模板复制
cp .env.example .env
vi .env

# 方法 2: 直接创建
cat > .env << 'EOF'
APP_PORT=5231
APP_TITLE=我的笔记
APP_CATEGORY=个人工具
EOF
```

然后重启：

```bash
docker-compose restart
```

### 3. 验证 WatchCow 标签

查看容器的 labels：

```bash
docker inspect memos | grep watchcow
```

应该看到类似输出：

```json
"watchcow.enable": "true",
"watchcow.appName": "memos",
"watchcow.title": "Memos",
"watchcow.port": "5230",
...
```

## WatchCow Labels 说明

### 必需标签

| Label | 说明 | 默认值 |
|-------|------|--------|
| `watchcow.enable` | 启用发现 | `"true"` |
| `watchcow.appName` | 应用唯一标识 | `"memos"` |
| `watchcow.title` | 显示标题 | `"Memos"` |
| `watchcow.port` | 外部端口 | `"5230"` |
| `watchcow.fnDomain` | fnOS 域名 | `"memos"` |

### 可选标签

| Label | 说明 | 默认值 |
|-------|------|--------|
| `watchcow.appID` | 应用 ID | `"1002"` |
| `watchcow.desc` | 应用描述 | `"轻量级笔记与知识管理工具"` |
| `watchcow.icon` | 图标 URL | memos 图标 URL |
| `watchcow.category` | 分类 | `"效率工具"` |
| `watchcow.protocol` | 协议 | `"http"` |
| `watchcow.path` | URL 路径 | `"/"` |
| `watchcow.microApp` | 是否为微应用 | `"false"` |
| `watchcow.nativeApp` | 是否为原生应用 | `"false"` |
| `watchcow.isDisplay` | 是否显示 | `"true"` |

## 配置文件说明

### compose.yaml

包含所有默认配置，使用 `${VAR:-default}` 语法：

```yaml
watchcow.title: "${APP_TITLE:-Memos}"  # 默认：Memos
watchcow.port: "${APP_PORT:-5230}"     # 默认：5230
```

**特点**：
- ✅ 默认值硬编码，无需配置文件即可启动
- ✅ 提交到 Git，作为标准配置
- ✅ 通过环境变量覆盖

### .env（可选）

用户自定义配置文件，用于覆盖默认值：

```env
# 只需要包含你想修改的配置
APP_PORT=5231
APP_TITLE=我的笔记本
DATA_PATH=/custom/path/memos_data
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

## 数据持久化

Memos 的所有数据（笔记、用户信息、配置等）存储在：

```
./volumes/memos_data/
```

**重要**：
- 🔒 备份此目录以保护你的数据
- 📦 迁移时复制此目录即可
- 🗑️ 删除容器不会删除数据

## 与 WatchCow 集成

### 1. 启动 WatchCow

```bash
cd /path/to/watchcow
docker-compose up -d
```

### 2. 启动 Memos

```bash
cd examples/memos
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

1. `compose.yaml`: `watchcow.port: "${APP_PORT:-5230}"`
2. 不创建 `.env`: 使用默认值 `5230`
3. 创建 `.env` 设置 `APP_PORT=5231`: 使用 `5231`

## 常见问题

### Q: 首次访问需要做什么？

**A:** 第一次访问 Memos 会要求创建管理员账号，设置用户名和密码即可。

### Q: 如何备份数据？

**A:** 备份 `./volumes/memos_data` 目录：
```bash
tar czf memos-backup-$(date +%Y%m%d).tar.gz volumes/memos_data/
```

### Q: 如何迁移数据？

**A:** 复制 `volumes/memos_data` 目录到新位置，或在 `.env` 中指定：
```env
DATA_PATH=/new/path/memos_data
```

### Q: 如何使用 HTTPS？

**A:** Memos 本身不支持 HTTPS，建议使用反向代理（如 Nginx）。或者在 `.env` 中配置（如果你的反向代理配置了 HTTPS）：
```env
APP_PROTOCOL=https
APP_PORT=443
```

### Q: 数据存储在哪里？

**A:** Memos 使用 SQLite 数据库，所有数据存储在 `/var/opt/memos` 目录（映射到宿主机的 `./volumes/memos_data`）。

## 自定义示例

### 示例 1: 修改端口

```bash
# .env
APP_PORT=5231
```

### 示例 2: 完全自定义

```bash
# .env
APP_NAME=my-memos
APP_TITLE=我的笔记本
APP_DESC=个人知识管理系统
APP_PORT=5231
APP_CATEGORY=个人工具
APP_ICON=https://example.com/my-icon.png
DATA_PATH=/data/memos
```

### 示例 3: 隐藏应用

```bash
# .env
APP_IS_DISPLAY=false
```

## 参考资料

- [Memos 官方文档](https://usememos.com/docs)
- [Memos GitHub](https://github.com/usememos/memos)
- [fnOS 应用规范](https://docs.fnnas.com)
- [Docker Compose 文档](https://docs.docker.com/compose/)
- [WatchCow 项目主页](https://github.com/tf4fun/watchcow)
