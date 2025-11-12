# Nginx Example - 测试指南

本文档指导如何测试 WatchCow 的 labels 自动发现功能。

## 测试环境准备

### 1. 启动 WatchCow

```bash
# 在项目根目录
cd /path/to/watchcow

# 构建并启动 WatchCow
docker-compose up -d

# 查看日志
docker-compose logs -f
```

### 2. 启动 Nginx 示例应用

```bash
# 进入示例目录
cd examples/nginx

# 启动应用（默认配置）
# 注意：即使 override.env 不存在也不会报错（required: false）
docker-compose up -d

# 查看容器状态
docker ps | grep nginx-example
```

## 测试步骤

### 测试 1: 验证 Labels 配置

查看容器的 watchcow labels：

```bash
docker inspect nginx-example | grep -A 20 "Labels"
```

**预期输出**：
```json
"Labels": {
    "watchcow.enable": "true",
    "watchcow.appName": "nginx.example",
    "watchcow.appID": "nginx-001",
    "watchcow.title": "Nginx 示例",
    "watchcow.desc": "Nginx Web 服务器示例应用",
    "watchcow.icon": "https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/nginx.png",
    "watchcow.category": "开发工具",
    "watchcow.protocol": "http",
    "watchcow.port": "8080",
    "watchcow.path": "/",
    "watchcow.fnDomain": "nginx-example",
    ...
}
```

### 测试 2: 验证 WatchCow 发现

查看 WatchCow 日志：

```bash
docker logs watchcow | tail -20
```

**预期日志**：
```
▶️  Container started: nginx-example
✅ Discovered app with labels: nginx.example
📝 App Config:
   - Title: Nginx 示例
   - Port: 8080
   - Domain: nginx-example
   - Category: 开发工具
```

### 测试 3: 验证应用可访问

```bash
# 访问应用
curl http://localhost:8080

# 或在浏览器打开
open http://localhost:8080
```

**预期结果**：
- 看到 WatchCow 示例页面
- 显示应用信息和状态

### 测试 4: 自定义配置

编辑 `override.env`：

```bash
cat > override.env << EOF
# 自定义配置测试
APP_PORT=9090
APP_TITLE=我的 Nginx 测试
APP_CATEGORY=测试分类
APP_ICON=https://example.com/custom-icon.png
EOF
```

重新启动容器：

```bash
docker-compose down
docker-compose up -d
```

查看 WatchCow 日志，应该看到更新的配置：

```bash
docker logs watchcow | grep "nginx"
```

**预期**：
- 端口变为 9090
- 标题变为 "我的 Nginx 测试"
- 分类变为 "测试分类"

### 测试 5: 禁用显示

测试 `isDisplay` 标志：

```bash
cat > override.env << EOF
APP_IS_DISPLAY=false
EOF

docker-compose restart
```

**预期结果**：
- WatchCow 仍然发现应用
- 但 `isDisplay` 为 false，应用不会在 fnOS 应用列表显示

### 测试 6: 应用停止/重启

```bash
# 停止应用
docker-compose stop

# 查看 WatchCow 日志
docker logs watchcow | tail -5
```

**预期日志**：
```
⏹️  Container stopped: nginx-example
📡 Sent stopped notification to fnOS
```

重新启动：

```bash
# 启动应用
docker-compose start

# 查看日志
docker logs watchcow | tail -5
```

**预期日志**：
```
▶️  Container started: nginx-example
📡 Sent running notification to fnOS
```

## 高级测试

### 测试 7: 无 Labels 的容器（自动发现）

创建一个没有 watchcow labels 的容器：

```bash
docker run -d --name test-no-labels -p 8888:80 nginx:alpine
```

**预期行为**：
- WatchCow 使用自动发现模式
- 应用名称：docker-test-no-labels
- 标题：Test No Labels（自动美化）
- 图标：nginx 默认图标
- 分类：Docker

查看发现结果：

```bash
docker logs watchcow | grep "test-no-labels"
```

清理：

```bash
docker rm -f test-no-labels
```

### 测试 8: 容器无端口暴露

启动一个无端口的容器：

```bash
docker run -d --name test-no-port \
  -l watchcow.enable=true \
  -l watchcow.title="无端口应用" \
  nginx:alpine
```

**预期行为**：
- WatchCow 发现容器
- 因为没有端口，跳过注册
- 日志显示："Skipped (no port)"

清理：

```bash
docker rm -f test-no-port
```

### 测试 9: Boolean 标签解析

测试各种 boolean 值：

```bash
# 测试 true
docker run -d --name test-bool-1 -p 8001:80 \
  -l watchcow.enable=true \
  -l watchcow.microApp=true \
  nginx:alpine

# 测试 1
docker run -d --name test-bool-2 -p 8002:80 \
  -l watchcow.enable=true \
  -l watchcow.microApp=1 \
  nginx:alpine

# 测试 yes
docker run -d --name test-bool-3 -p 8003:80 \
  -l watchcow.enable=true \
  -l watchcow.microApp=yes \
  nginx:alpine
```

验证：

```bash
docker logs watchcow | grep -E "test-bool"
```

**预期**：所有容器的 `microApp` 都应该为 `true`

清理：

```bash
docker rm -f test-bool-1 test-bool-2 test-bool-3
```

## 故障排查

### 问题 1: WatchCow 未发现容器

**检查清单**：
- [ ] WatchCow 是否运行: `docker ps | grep watchcow`
- [ ] 容器是否有 `watchcow.enable=true`: `docker inspect <container> | grep watchcow.enable`
- [ ] WatchCow 日志: `docker logs watchcow`

### 问题 2: Labels 未生效

**原因**: Docker Compose 可能缓存了环境变量

**解决**：
```bash
docker-compose down
docker-compose up -d --force-recreate
```

### 问题 3: 端口配置错误

**检查**：
```bash
# 查看容器实际监听的端口
docker port nginx-example

# 查看 labels 中的端口
docker inspect nginx-example | grep watchcow.port
```

### 问题 4: 图标不显示

**原因**：图标 URL 可能无法访问

**测试**：
```bash
curl -I "$(docker inspect nginx-example | grep watchcow.icon | cut -d'"' -f4)"
```

## 性能测试

### 大量容器测试

创建 10 个容器：

```bash
for i in {1..10}; do
  docker run -d --name "test-app-$i" -p "808$i:80" \
    -l watchcow.enable=true \
    -l watchcow.appName="test.app.$i" \
    -l watchcow.title="测试应用 $i" \
    nginx:alpine
done
```

观察 WatchCow 性能：

```bash
docker stats watchcow
docker logs watchcow | grep "Discovered"
```

清理：

```bash
for i in {1..10}; do
  docker rm -f "test-app-$i"
done
```

## 集成测试

### 完整流程测试

```bash
# 1. 启动 WatchCow
cd /path/to/watchcow
docker-compose up -d

# 2. 启动示例应用
cd examples/nginx
docker-compose up -d

# 3. 等待 2 秒
sleep 2

# 4. 验证发现
docker logs watchcow | grep "nginx-example"

# 5. 访问应用
curl -s http://localhost:8080 | grep "WatchCow"

# 6. 修改配置
echo "APP_TITLE=测试成功" > override.env
docker-compose restart

# 7. 验证更新
sleep 2
docker logs watchcow | grep "测试成功"

# 8. 停止应用
docker-compose stop

# 9. 验证停止通知
docker logs watchcow | grep "stopped.*nginx-example"

# 10. 清理
docker-compose down
```

**如果所有步骤都成功，说明 WatchCow 功能正常！** ✅

## 报告问题

如果测试失败，请收集以下信息：

```bash
# 系统信息
uname -a
docker version

# WatchCow 日志
docker logs watchcow > watchcow.log

# 容器信息
docker inspect nginx-example > nginx-example.json

# 网络信息
docker network inspect bridge
```

提交 Issue 时附上这些文件。
