# DBVault Release Notes Standard & Template Guide

Tài liệu này hướng dẫn chuẩn hóa cấu trúc **Release Title** và **Release Notes (Description)** trên GitHub Release cho DBVault, tuân thủ nghiêm ngặt theo [Chính sách Quản trị Phát hành (Release Governance Policy)](releasing.md).

---

## 1. Quy tắc đặt Tiêu đề (Release Title)

Tiêu đề Release **phải** khớp chính xác với Tag SemVer đang phát hành:

```text
DBVault vX.Y.Z
```

- **Ví dụ đúng:** `DBVault v0.1.0`, `DBVault v0.2.0`, `DBVault v1.0.0`
- **Tuyệt đối không dùng:** `Release v1.0.0` (khi tag là v0.1.0), `v0.1.0`, `Update 1`, `Final Release`.

---

## 2. Cấu trúc chuẩn của Release Notes (Description)

Mỗi bản phát hành cần có đầy đủ các phần sau (bỏ qua các mục không có thay đổi, không để placeholder trống):

```markdown
# DBVault vX.Y.Z

Tóm tắt ngắn gọn 1-2 câu về mục tiêu, ý nghĩa và trọng tâm của bản phát hành này.

## Highlights

- Điểm nổi bật 1 (tính năng cốt lõi hoặc cập nhật kiến trúc lớn)
- Điểm nổi bật 2 (cải thiện hiệu năng hoặc độ ổn định)

## Added

- Danh sách các tính năng, lệnh CLI, storage provider hoặc database adapter mới được thêm vào.
- Trích xuất trực tiếp từ mục `Added` trong CHANGELOG.md tương ứng với phiên bản.

## Changed

- Những thay đổi trong hành vi hiện tại, giá trị mặc định của config hoặc CLI flags.
- Trích xuất từ mục `Changed` trong CHANGELOG.md.

## Fixed

- Các lỗi đã được sửa chữa, xử lý race condition, vá lỗi kết nối hoặc rò rỉ tài nguyên.

## Security

- Vá bảo mật, gia cố quyền hạn filesystem (0600/0700), che giấu secret (redaction).

## Breaking Changes (Nếu có)

- Mô tả chi tiết những thay đổi không tương thích ngược và hành động cần thiết từ phía người vận hành/devops.
- Trong giai đoạn `v0.x`, mọi breaking change đều phải gắn nhãn rõ ràng.

## Installation

### Via Go Install
```bash
go install github.com/sung2708/DBVault/cmd/dbvault@vX.Y.Z
```

### Verify Installation
```bash
dbvault version
dbvault --help
```

> **Lưu ý:** Nêu rõ các công cụ database native phụ thuộc (nếu cần như pg_dump, mysqldump, mongodump).

## Documentation

- [Getting Started](https://github.com/sung2708/DBVault/blob/vX.Y.Z/docs/getting-started.md)
- [CLI Reference](https://github.com/sung2708/DBVault/blob/vX.Y.Z/docs/cli-reference.md)
- [Configuration](https://github.com/sung2708/DBVault/blob/vX.Y.Z/docs/configuration.md)

## Checksums / Artifacts

Verify asset integrity using the published `checksums.txt`:
```bash
sha256sum -c checksums.txt --ignore-missing
```

## Full Changelog

https://github.com/sung2708/DBVault/compare/vPREVIOUS...vX.Y.Z
```

---

## 3. Quy trình trích xuất nhanh từ CHANGELOG.md

Khi chuẩn bị release, toàn bộ nội dung chi tiết cho các mục `Added`, `Changed`, `Fixed`, `Security` đã được chuẩn bị và đóng băng trong [CHANGELOG.md](../CHANGELOG.md) tại tiêu đề:

```markdown
## [vX.Y.Z] - YYYY-MM-DD
```

Chỉ cần sao chép các gạch đầu dòng tương ứng vào template trên để đảm bảo tính đồng bộ 100% giữa mã nguồn, tài liệu và GitHub Release.
