# Windows TLS 信任包

[English](TRUST.en.md) · [接入说明](INTEGRATION.zh-CN.md)

## r2 修复了什么

旧采集器在初始化时将 ca.pem 写成单张 observer CA。部分 TLS 客户端会把 SSL_CERT_FILE 当作系统根证书的替代来源，因此公共服务的证书链可能验证失败，虽然采集代理本身仍在运行。

0.4.9-r2 在原生成点合并 observer CA 和 Windows ROOT 中允许 TLS 服务端验证的公开证书。按 DER SHA-256 去重并稳定排序；读取 CurrentUser 和 LocalMachine 存储均为只读。identity.json 中的签发 CA、私钥和代理身份保持不变。包内不携带作者机器的任何 CA 或凭据。

## 生成与写入

只在采集初始化时枚举系统根，没有新轮询器。相同内容不重写；变化通过同目录完整文件替换，保留已有 DACL，并准备旧文件恢复点。生成失败不会把现有包降回单 CA。

PEM 是初始化时的根证书快照，不能表达 Windows 的全部链策略、撤销策略或自动根下载机制。系统根变更在下次采集初始化时反映。它不关闭证书验证，也不向系统根存储导入 observer CA。

实现位于 deployment/collector/native_trust_windows.go，由 deployment/build-overlay.cjs 投影到原 writeNativeHome。直接编译 native/ 仍得到保留的旧基线；正常公开构建入口生成带修复的 watcher。

## 现有安装怎样更新

1. 保留当前可用程序、identity.json、有效完整 ca.pem 和自己的配置。
2. 核对运行包 SHA256SUMS，更新属于这套安装的程序。保留原目录布局及签发身份。
3. 常驻 watcher 已运行时，仅替换磁盘文件不会让当前进程加载新代码。选择自己的维护时机正常重启 watcher；如果使用开机启动，也可等下一次正常开机。单独退出再打开 Codex 不保证 watcher 重启。
4. 确认新 watcher 已加载，ca.pem 仍包含原 observer CA 与当前系统根，再观察原客户端连接。

升级过程涉及采集代理，按自己的维护授权执行。公开打包脚本不启动程序、不安装任务、不修改连接配置。

若回退旧 watcher，应保留有效完整信任包；旧程序再次初始化仍可能覆盖它，需要在旧程序初始化后恢复该包。这是临时回退，不是持久修复。

## 验证与来源

定点检查覆盖两种签发根的 TLS 链、用途过滤、重复初始化、原身份与设置保留、错误输入和读者占用导致的替换失败。独立 Windows/Python 枚举与成功恢复连接的公共根集合一致；接收方证书数量取决于自己的系统。

- [CertOpenStore](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certopenstore)
- [CertGetEnhancedKeyUsage](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certgetenhancedkeyusage)
- [ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)
