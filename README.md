# font-raster-subset-go

离线字体子集化与光栅化引擎（纯 Go，无外部依赖）。

- **font**：解析 TrueType（sfnt）字体，暴露度量、字符映射、字距与字形轮廓。
- **subset**：按字符集合生成结构完整、可被其他加载器读取的子集字体。
- **raster**：把字形按指定像素尺寸渲染为带抗锯齿的灰度位图。
- **engine**：并发安全的批量子集化/光栅化，支持取消与原子文件输出。
- **cmd/fonttool**：命令行工具。

## 快速开始

```go
f, err := font.Parse(data)            // 解析字体（*Font 不可变，可并发使用）
m := f.Metrics()                      // UnitsPerEm / Ascender / Descender / LineGap
g, ok := f.GlyphIndex('A')            // 字符 -> 字形索引
adv := f.AdvanceWidth(g)              // 前进宽度（字体单位）
b := f.GlyphBounds(g)                 // 包围盒（字体单位）
kern := f.Kern(left, right)           // 字距调整（字体单位）

sub, err := subset.Subset(f, subset.Options{Runes: []rune("Hello世界")})

img, err := raster.Glyph(f, g, raster.Params{Size: 32})
```

命令行：

```sh
fonttool subset -font in.ttf -out subset.ttf -chars "Hello世界"
fonttool raster -font subset.ttf -chars "Hi" -size 48 -outdir glyphs/
```

## 接口语义

### 解析（font.Parse）

- 支持 TrueType 轮廓（glyf）字体；CFF（OTTO）、TTC 合集返回 `ErrUnsupported`。
- 错误可区分：`ErrNotFont`（不是字体）、`ErrMalformed`（结构损坏/截断）、
  `ErrUnsupported`（合法但未支持的特性）。任何损坏输入都不会 panic，
  也不会静默返回错误结果。
- cmap 优先选择 format 12（全 Unicode），其次 format 4（BMP）。

### 度量

`Metrics`、`AdvanceWidth`、`GlyphBounds` 均以字体单位返回；换算到像素
需乘以 `size / UnitsPerEm`。

### 子集化（subset.Subset）

- 始终保留 `.notdef`（glyph 0）。
- **字符集合之外的行为**：未在源字体中映射的字符被静默跳过（不报错）；
  子集中不存在的字符在子集字体里同样无映射。可用 `GlyphIndex` 预先检查。
- 复合字形的全部传递依赖会被纳入子集，组件引用整体重映射，
  **不会留下悬空引用**。
- 布局表（GSUB/GPOS/GDEF 等）被丢弃：保留字符的轮廓渲染结果与源字体
  逐点一致，但不再执行 shaping。`kern` 表默认保留并过滤重映射
  （可用 `Options.KeepKern=false` 关闭）。
- `post` 表重写为 format 3.0（不含字形名），`head`/`hhea`/`maxp`/`loca`
  按新字形集合重建，校验和（含 `checkSumAdjustment`）重新计算。
- `Options.MaxGlyphs` 限制输出字形数（默认 65535），超限返回
  `ErrTooManyGlyphs`。

### 光栅化（raster.Glyph）

- 非零绕向填充；8×8 超采样抗锯齿（256 级灰度）。
- 二次贝塞尔按固定平坦度容差（0.25px）自适应细分——**缩放与填充行为
  完全确定**，同一输入在任意尺寸、任意并发度下输出逐字节一致。
- 返回图像的 `Bounds().Min` 位于字体基线坐标系中（y 向下），同一字体
  同一尺寸的字形位图共享坐标系，可直接按前进宽度排版拼接。
- 空字形返回空图像；`Params.MaxDim`（默认 4096）限制位图边长，
  超限返回 `ErrTooLarge`，防止大字体/大尺寸耗尽内存。

### 字距（Font.Kern）

- 存在 kern 表（format 0，水平）时按数据生效。
- 表缺失、字对缺失、或源字体中含越界字形引用的条目被丢弃时，
  回落到 **0**（确定且可解释）。未知的 kern 版本返回 `ErrUnsupported`。

### 批量处理（engine）

- `BatchRasterize` / `BatchSubset`：有界并发（`workers` 参数），结果按
  任务顺序返回；**输出不随并发度变化**。
- `context` 取消后不再调度新任务；未开始任务的结果携带 `ctx.Err()`。
- 文件输出均为原子写（临时文件 + fsync + rename）：取消、超时或进程
  崩溃都不会留下写了一半的文件。
- 内存上限：解析阶段限制表数量/字形数/点数/复合嵌套深度；光栅化受
  `MaxDim` 限制；子集化受 `MaxGlyphs` 限制。

### 确定性

相同输入与相同配置下，子集文件字节与位图像素逐次一致：表按标签排序
写出、字形按 ID 排序、字形内点序保持源顺序、零填充、无随机源、无
时间戳。复现历史结果只需固定输入与参数。

## 测试

```sh
go test ./...          # 全部测试
go test ./... -race    # 含竞态检测
```

覆盖：损坏/变异输入（不 panic、错误可分类）、复合字形与依赖闭包、
字符集合缺失、子集往返渲染逐像素一致、并发确定性、取消与原子写。
部分测试使用系统 DejaVu 字体，缺失时自动跳过。

## 限制

- 仅支持 TrueType 轮廓（glyf）；不支持 CFF/CFF2、可变字体、彩色字体。
- 复合字形仅支持 XY 偏移参数（point-matching 参数返回 `ErrUnsupported`）。
- 子集不保留 shaping 布局表（GSUB/GPOS）。
