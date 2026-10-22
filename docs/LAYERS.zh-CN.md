# 六层工程

[English](LAYERS.en.md) · [返回首页](../README.md)

六层属于同一份 0.4.9 工程，共享时间轴和数据布局。浏览器打开 `visual/index.html` 后，右侧“图层”区可开关或独显各层；独显保留当前运动状态。前景泛光独显仍使用其他层的发光输入，输出的是光层而非完整底色。

| 图层 | 修改入口 | 主要内容 |
| --- | --- | --- |
| 01 玻璃背景 | `visual/background/engine/`、`visual/engine/glass_background.js` | 几何、材质、光学、升降、阵列与镜头 |
| 02 流动曲线 | `engine/flow.js`、`ribbon_art.js`、`ribbons.js`、`shaders/scene_rib.js` | 长波、分支、数量层级和流动 |
| 03 漂浮光点 | `engine/particles.js`、`shaders/post.js` | 有界光点与碎光 |
| 04 外框刻度 | `engine/hud_motion.js`、`shaders/scene_hud.js` | 外框、身份短线、刻度、展开端点 |
| 05 信息文字 | `engine/atlas.js`、`labels.js`、`row_presentation.js`、`shaders/text.js` | 逐字入场、模型与速度布局、流光和字形图集 |
| 06 前景泛光 | `engine/renderer.js`、`shaders/post.js`、`presentation.js` | 发光、多级 bloom、入场暖光与外缘收束 |

除表中第一项外，相对入口均位于 `visual/` 下。六份 [`layers/`](../layers/) 清单列出完整的共享关系与 SHA-256；[`layers.lock.json`](../layers.lock.json) 锁定所有 39 个运行脚本。共享文件只维护一份。

## 参数与动作顺序

主要参数在 [`presentation.js`](../visual/engine/presentation.js)，玻璃曲线在 [`appearance.js`](../visual/background/engine/appearance.js)。下表记录当前值，不应把单个局部参数误读为整段动画的总时长。

| 项目 | 当前值或行为 |
| --- | --- |
| 实际渲染余量 | 每边 128 DIP |
| 外缘柔化 | 面板外 120 DIP 的五次曲线收束 |
| 入场暖光 | 保留光场峰值；0.42 秒聚合；退出独立暖光为零 |
| 模型行 | 完整名字出现后停留 0.18 秒，再展开 |
| 展开 / 收拢 | 0.60 / 0.50 秒；端点从名字附近向两侧运动 |
| 输出起步 / 暂停保留 | 0.12 / 0.80 秒 |
| 数字 | 0.65 秒有界切线插值，按已呈现帧刷新一位小数 |
| 输出文字光效 | 宽柔晕、主/伴随流光、错相微闪，复用字形图集 |
| 无模型镜头 | 自动选择空档案镜头，近景倍率 1.08；有模型时平滑拉远 |
| 玻璃 | 局部 rise 1.22 秒，阵列深度延迟跨度 0.70 秒；带有限尾摆 |
| 曲线数量 | 保留长波，在 2 和 4 个会话节点增强可见增长 |

顺序由 `animator.js`、`stage.js`、`row_presentation.js` 和玻璃控制器共同保证：模型名完整出现 → 需要时展开 → 输出停止后收拢 → 收拢完成再消散。玻璃的短暂预抬可以与收拢末尾重叠；真正下落等待收拢。连续变化合并到最新目标，已有动作在有限端点完成，不积累无限历史队列。

## 修改与检查

修改唯一源文件，然后运行基础视觉、当前视觉与工程约束检查。更新锁清单时记录对应艺术变化，再生成六份层清单；哈希一致只证明文件一致，实际像素和动作顺序仍由浏览器检查。

```powershell
npm test
npm run test:current
npm run test:engineering
```

运行代码使用共享 WebGL2 上下文、有限纹理/图集资源、分时字形准备和单待绘帧。Canvas 回退能保留信息显示，但不承诺完整复现 WebGL 的艺术效果。静态模式保留状态更新，同时停止装饰运动。
