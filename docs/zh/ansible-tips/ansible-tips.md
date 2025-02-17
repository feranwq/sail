# Ansible Tips

## 通用原则

### 脚本的执行尽可能与执行脚本时的当前所在目录保持解耦

大部分命令如果都需要操作员去小心翼翼地关注当前所在目录的话，就会很容器出错。

脚本中处理任何文件时，都要使用绝对路径，或者基于脚本所在目录去「定位」文件。

Bash 脚本中可以在脚本的开头使用下面方式取得脚本的所在目录：

```bash
#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
```

### 不要相信在线安装源，永远指定版本

## Ansible Tasks 的编写原则

- 实现幂等（或者「伪」幂等）可重复执行
- 尽可能避免将脚本文件（如 .sh, .py）通过模板渲染。脚本的可变部分转换成脚本的参数或配置文件，通过 ansible 去渲染配置文件或渲染执行的命令。
