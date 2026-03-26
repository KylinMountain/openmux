# OpenMux 快速开始指南

## 1. 安装

确保已安装 Go 1.21 或更高版本。

```bash
# 下载依赖
go mod tidy
# 构建
make build
```

## 2. 配置

```bash
cp config.yaml.example config.yaml
cp .env.example .env
```

编辑 `.env` 文件，填入你拥有的平台 API Key（不用的留空即可）：

```bash
# 免费平台 - 注册即可用
ZHIPU_API_KEY=xxx        # 智谱 GLM https://open.bigmodel.cn
SILICONFLOW_API_KEY=xxx  # 硅基流动 https://cloud.siliconflow.cn
OPENROUTER_API_KEY=xxx   # OpenRouter https://openrouter.ai/keys
```

> 只需填入一个 Key 就能用起来，多个平台配合使用可实现自动 fallback。

## 3. 运行

```bash
# 直接运行
./bin/openmux -config config.yaml

# 或使用 Docker Compose（推荐）
docker-compose up -d
```

## 4. 测试 API

### 使用免费模型聊天

```bash
# 使用预配置的免费模型路由（自动 fallback 到可用平台）
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "chat-free", "messages": [{"role": "user", "content": "你好"}]}'
```

### 直通模式访问指定模型

```bash
# provider/model 格式直接访问
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "zhipu/glm-4-flash", "messages": [{"role": "user", "content": "你好"}]}'

# 使用硅基流动的免费模型
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "siliconflow/Qwen/Qwen2.5-7B-Instruct", "messages": [{"role": "user", "content": "你好"}]}'
```

### 嵌入 (Embedding)

```bash
curl http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model": "zhipu/embedding-3", "input": "hello"}'
```

## 5. 使用 Docker (推荐)

```bash
# 直接运行最新镜像
docker run -p 8080:8080 \
  --env-file .env \
  -v $(pwd)/config.yaml:/app/config.yaml \
  ghcr.io/evilkylin/openmux:latest
```

## 6. 预配置模型路由

| 路由名称 | 说明 | 平台 |
|----------|------|------|
| `chat-free` | 免费聊天模型，多平台 fallback | 智谱 + 硅基流动 + OpenRouter |
| `reasoning-free` | 免费推理模型 | 硅基流动 + OpenRouter |
| `chat` | 通用大模型（付费） | DeepSeek + 阿里云 + 智谱 |
| `glm-4-flash` | GLM-4-Flash 多云部署 | 智谱 + 硅基流动 |

## 7. 开启精准限流

OpenMux 内置了 `tiktoken` 支持。只需在 `providers` 下配置 `tpm`，系统就会根据不同的分词器精准计算每个请求的消耗。

---
更多详细配置请参考 [README.md](README.md)。
