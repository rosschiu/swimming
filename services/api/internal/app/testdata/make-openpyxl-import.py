# Regenerates openpyxl-import.xlsx: python -I make-openpyxl-import.py openpyxl-import.xlsx (needs openpyxl).
import sys, datetime, openpyxl
wb = openpyxl.Workbook()
info = wb.active; info.title = "Notes"; info["A1"] = "Read me first"
ws = wb.create_sheet("Students", 0)  # first sheet
ws.append(["Student name", "Level", "Guardian name", "Guardian email", "Second guardian name", "Second guardian email", "Credits", "Credits expire on"])
ws.append(["Real Excel 泳", "Beginner", "Excel Parent", "excel.parent@swim.test", None, None, 12, datetime.date(2027, 3, 31)])
ws.append(["Second Row", "Improver", "Excel Parent", "excel.parent@swim.test", None, None, None, None])
ws.append([None]*8)
ws.append(["Gap Row", "Beginner", None, None, None, None, 2, "2027-01-15"])
wb.save(sys.argv[1])
