# CreditLink Backend

CreditLink 链上信用借贷协议后端服务。

## 技术栈

- **语言**: Go 1.21+
- **Web框架**: Gin
- **数据库**: PostgreSQL + GORM
- **缓存**: Redis
- **认证**: JWT + 以太坊签名验证
- **区块链**: go-ethereum

## 功能模块

- 用户管理与钱包绑定
- 信用评分计算与存储
- 签名生成与验证
- 借贷活动记录
- 风险参数管理
- 区块链事件索引

## 项目结构

```
backend/
├── cmd/
│   └── server/         # 应用入口
├── configs/            # 配置文件
├── internal/
│   ├── api/           # HTTP API 处理器
│   ├── config/        # 配置加载
│   ├── database/      # 数据库连接
│   ├── middleware/    # 中间件
│   ├── models/        # 数据模型
│   ├── repository/    # 数据访问层
│   └── service/       # 业务逻辑层
├── migrations/        # 数据库迁移
└── pkg/               # 公共包
    └── utils/         # 工具函数
```

## 开发

```bash
# 安装依赖
go mod download

# 运行服务
go run cmd/server/main.go

# 构建
go build -o bin/creditlink-backend cmd/server/main.go

# 运行测试
go test ./...
```

## 配置

复制配置文件模板：

```bash
cp configs/config.example.yaml configs/config.yaml
```

主要配置项：

```yaml
server:
  port: 8080
  mode: debug

database:
  host: localhost
  port: 5432
  name: creditlink
  user: postgres
  password: your-password

redis:
  host: localhost
  port: 6379

jwt:
  secret: your-jwt-secret
  expire: 24h

chain:
  rpc_url: https://eth-sepolia.g.alchemy.com/v2/your-api-key
  chain_id: 11155111
  lending_pool: "0x..."
  risk_registry: "0x..."
```

后端启动时会从 `RiskRegistry` 的 `getAssetsList()` 和
`getRiskParams(asset)` 自动同步 `current_risk_params`，运行期间每 30 秒刷新一次。
价格由风险参数中的 `PriceOracleAdapter` 地址实时读取；同步或链上读取失败时，
USD 估值接口会返回错误，不会回退到固定价格。

## API 端点

### 认证
- `POST /api/v1/auth/nonce` - 获取登录 nonce
- `POST /api/v1/auth/login` - 钱包签名登录

### 用户
- `GET /api/v1/user/profile` - 获取用户信息
- `GET /api/v1/user/credit` - 获取信用评分

### 签名
- `POST /api/v1/signature/request` - 请求借贷签名
- `GET /api/v1/signature/history` - 签名历史

### 活动
- `GET /api/v1/activities` - 获取借贷活动记录

## License

MIT
