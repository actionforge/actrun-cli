# Actrun

Welcome to `actrun` for Python. To get started, import the package and execute within your Python environment.

You can run it within your system’s Python environment or an embedded one within a host application like Blender, Maya, or similar apps.

##### Example Code

```python
import sys
sys.path.append('/path/to/parent/of/actrun')

import actrun
actrun.run_graph("hello-world.act")
```

##### Output

```
Hello World! 🚀
Running within embedded Python environment.
Python version: 3.11.7
Executable path: /Applications/Blender/[...]/python/bin/python3.11
42
```

For further info, see the [First Steps](https://docs.actionforge.dev/first-steps/) section in the online documentation.
