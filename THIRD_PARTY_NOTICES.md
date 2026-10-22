# 第三方声明 / Third-party notices

本项目代码沿用 MIT；各第三方组件保留自己的许可和版权。配乐、原作视觉和品牌不由项目代码的 MIT 许可重新授权。

Project code uses MIT. Third-party components retain their own licenses and copyrights. The code license does not relicense music, original visual works, or brands.

| 组件 / Component | 许可与记录 / License and record |
| --- | --- |
| RhineLabUI · LBEILC | [MIT](visual/references/LICENSE.RhineLabUI.txt), [来源 / attribution](ACKNOWLEDGMENTS.md) |
| Go runtime | [Go license](native/licenses/Go-LICENSE.txt) |
| klauspost/compress and nested dependencies | [Native notices](native/THIRD_PARTY_NOTICES.md), original notices retained in `native/vendor/` |
| nlohmann/json 3.11.3 | [MIT](desktop/vendor/json-LICENSE.MIT) |
| Microsoft WebView2 SDK / loader | [License](desktop/vendor/WebView2-LICENSE.txt), [third-party notice](desktop/vendor/WebView2-NOTICE.txt) |
| Playwright | Development dependency; Apache-2.0. Installed separately through npm. / 开发依赖，Apache-2.0，通过 npm 单独安装。 |
| FFmpeg / imageio-ffmpeg | Rendering tools, installed separately; binaries are not bundled with this repository or desktop release. / 渲染工具，单独安装，二进制不随本仓库或桌面包分发。 |

系统 WebView2 Runtime 由用户机器提供。原生源码与 vendor 中的原始版权头均保留。

The user's system supplies WebView2 Runtime. Original copyright headers in native source and vendored dependencies are retained.
