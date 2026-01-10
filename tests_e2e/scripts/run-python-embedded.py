import sys
import os

dname = os.path.dirname(__file__)
sys.path.append(os.path.join(dname, "..", "..", "api", "python"))

import actrun
actrun.run_graph("run-python-embedded-no-return.act", os.path.join(dname, "run-python-embedded-no-return.act"))
actrun.run_graph("run-python-embedded-return.act", os.path.join(dname, "run-python-embedded-return.act"))
actrun.run_graph("run-python-embedded-return-wrong-type.act", os.path.join(dname, "run-python-embedded-return-wrong-type.act"))
