{
  description = "arisu — 基于 kei 框架的聊天机器人（内置 kei-plugin-persona「LLM 人格代理」插件）";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};

      devTools =
        pkgs: with pkgs; [
          go # 编译器 / go test / go vet
          gopls # LSP
          gotools # goimports / guru 等辅助工具
          golangci-lint # 静态检查（可选）
          delve # 调试器 dlv
          jq # 解析 mock 适配器的 JSON 载荷
          curl # 注入事件、观察发送
          python3 # 本地 LLM 桩服务（README「本地联调」）
          git
        ];
    in
    {
      # 本仓库的构建依赖同级 kei / kei-plugin-persona 检出（go.mod 的 replace ../kei
      # 与 ../kei-plugin-persona）。nix 沙箱内没有这两个目录，flake 里也拿不到它们：
      # flake 输入与 path 字面量都逃不出 store，所以不提供 packages/checks/apps——
      # 与 ../kei-plugin-persona 的做法一致。构建与测试都在 devShell 内用 go 做：
      #   nix develop --command go build -o bin/arisu .
      #   nix develop --command go test -race ./...
      devShells = forAllSystems (system: {
        default = (pkgsFor system).mkShell {
          name = "arisu";

          packages = devTools (pkgsFor system);

          # 只使用 devShell 提供的 Go，禁止 go 自动下载其它 toolchain。
          env.GOTOOLCHAIN = "local";

          shellHook = ''
            echo "arisu dev shell · $(go version)"
            echo "常用命令: go build ./... | go vet ./... | go test ./... | go test -race ./... | golangci-lint run"
            for dep in ../kei ../kei-plugin-persona; do
              if [ ! -d "$dep" ]; then
                echo "缺少同级检出 $dep（go.mod 的 replace 依赖它）："
                echo "  git clone https://github.com/RandomLemon/$(basename "$dep") $dep"
              fi
            done
          '';
        };
      });

      # nixfmt-tree 让 `nix fmt` 在无参数时也能格式化目录树下的所有 .nix 文件。
      formatter = forAllSystems (system: (pkgsFor system).nixfmt-tree);
    };
}
