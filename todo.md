docker/virtalize/isolationn
move to rust/python/go
我觉得仅仅 context 隔离可能还不够，claude code 是不是经常会自己跑各种测试来验证功能，这样的话是不是给每一个
  worktree/pr/worker 单独配一个环境比较好。如果是的话有没有
