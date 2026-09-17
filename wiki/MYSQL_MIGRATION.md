# SQLite 迁移到 MySQL

Epusdt 可以复用同一台 MySQL 服务器或同一个 MySQL 容器，但必须使用独立数据库和独立账号。不要与 New API 共用数据库名或数据库账号。

以下示例只展示占位值，不要把真实密码提交到仓库：

```sql
CREATE DATABASE epusdt CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'epusdt'@'%' IDENTIFIED BY '<strong-random-password>';
GRANT ALL PRIVILEGES ON epusdt.* TO 'epusdt'@'%';
FLUSH PRIVILEGES;
```

目标 `.env` 使用 MySQL 作为主数据库，并让运行时锁和 EVM 扫描游标复用主数据库：

```dotenv
db_type=mysql
mysql_host=new-api-mysql
mysql_port=3306
mysql_user=epusdt
mysql_passwd=<strong-random-password>
mysql_database=epusdt
mysql_table_prefix=
runtime_db_type=primary
```

如果 MySQL 位于另一个 Docker Compose 项目，需把 Epusdt 服务加入 MySQL 所在的外部 Docker 网络。不要把 MySQL 的 3306 端口暴露到公网。

## 迁移步骤

1. 停止 Epusdt，确保迁移期间没有新订单、链上扫描或回调写入。
2. 备份主 SQLite 文件和运行时 SQLite 文件。
3. 创建空的 `epusdt` 数据库及专用账号，并准备指向它的目标 `.env`。
4. 使用新版本二进制执行迁移：

```bash
./epusdt --config /path/to/mysql.env migrate sqlite-to-mysql \
  --source-primary /path/to/epusdt.db \
  --source-runtime /path/to/runtime.db
```

迁移命令只接受空目标库，会复制主业务表、运行时锁和扫描游标，并根据历史已支付订单建立交易幂等记录。输出仅包含各表行数，不输出数据库密码。

5. 对照迁移输出检查关键表行数，再以目标 `.env` 启动 Epusdt。
6. 验证管理后台、创建订单、订单查询和回调，然后保留 SQLite 备份直至观察期结束。

回滚时先停止新版本，恢复原 SQLite 配置和文件后再启动。不要在 MySQL 与 SQLite 两套数据库之间同时运行两个写入实例。
