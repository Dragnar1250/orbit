# Builds tr.json and ru.json from the tr_ru_*.py tables.  python3 build.py
import json, sys, os
sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tr_ru_1 import T1
from tr_ru_2 import T2
from tr_ru_3 import T3
FIX = {"Mono": ("Mono", "Моно")}  # theme and font group share the word
tr, ru, seen = {}, {}, {}
for e, t, r in T1 + T2 + T3:
    t, r = FIX.get(e, (t, r))
    if e in seen and seen[e] != (t, r):
        print("different twice:", e, seen[e], (t, r))
    seen[e] = (t, r)
    tr[e], ru[e] = t, r
d = os.path.dirname(os.path.abspath(__file__))
for name, m in (("tr", tr), ("ru", ru)):
    with open(os.path.join(d, name + ".json"), "w", encoding="utf-8") as f:
        json.dump(m, f, ensure_ascii=False, indent=0, sort_keys=True)
print(len(tr), "strings")
