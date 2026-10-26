"""Regenerate the identical PDF fixtures shipped by the backend and frontend.

Requires reportlab. Run from any directory with: python3 scripts/generate-test-document.py
"""

from io import BytesIO
import hashlib
import json
from pathlib import Path

from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas


ROOT = Path(__file__).resolve().parents[1]
pdfmetrics.registerFont(TTFont("Demo", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"))
output = BytesIO()
pdf = canvas.Canvas(output, pagesize=A4, invariant=1, pageCompression=1)
pdf.setTitle("TEST - тестовый финансовый документ")
pdf.setAuthor("Credit cabinet demo")
width, height = A4
pdf.setFillColor(colors.HexColor("#EF3124"))
pdf.rect(42, height - 116, width - 84, 66, fill=1, stroke=0)
pdf.setFillColor(colors.white)
pdf.setFont("Demo", 20)
pdf.drawString(58, height - 78, "TEST - тестовый документ")
pdf.setFont("Demo", 10)
pdf.drawString(58, height - 99, "Демонстрационный образец. Не участвует в скоринге.")
pdf.setFillColor(colors.HexColor("#252525"))
pdf.setFont("Demo", 15)
pdf.drawString(42, height - 158, "Финансовые показатели за 2025 год")
pdf.setFont("Demo", 10)
pdf.drawString(42, height - 183, "Организация: учебная компания TEST")
pdf.drawString(42, height - 202, "Все суммы синтетические, в рублях.")
rows = [
    ("Выручка", "41 600 000,00"),
    ("Чистая прибыль", "5 200 000,00"),
    ("Активы", "30 000 000,00"),
    ("Капитал", "18 000 000,00"),
    ("Долгосрочные обязательства", "8 000 000,00"),
    ("Краткосрочные обязательства", "4 000 000,00"),
]
for index, (label, amount) in enumerate(rows):
    y = height - 246 - index * 36
    if index % 2 == 0:
        pdf.setFillColor(colors.HexColor("#F3F4F5"))
        pdf.rect(42, y - 12, width - 84, 36, fill=1, stroke=0)
    pdf.setFillColor(colors.HexColor("#252525"))
    pdf.drawString(54, y, label)
    pdf.drawRightString(width - 54, y, amount)
pdf.setFont("Demo", 10)
for index, line in enumerate([
    "Файл предназначен для проверки загрузки документов в кабинет.",
    "Backend принимает этот образец без проверки финансовых данных и ЭП.",
    "Показатели из этого файла исключены из расчёта кредитного решения.",
    "Документ не подтверждает доходы, полномочия или достоверность данных.",
]):
    pdf.drawString(42, height - 510 - index * 22, line)
pdf.setFillColor(colors.HexColor("#666666"))
pdf.setFont("Demo", 9)
pdf.drawString(42, 42, "TEST / Синтетические данные / 1")
pdf.save()
for relative in [
    "data-collector/internal/testdocument/test-financial-document.pdf",
    "frontend/public/test-documents/test-financial-document.pdf",
]:
    path = ROOT / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(output.getvalue())
    print(relative)
metadata = ROOT / "frontend/src/shared/lib/testDocumentMetadata.json"
metadata.write_text(json.dumps({
    "file_name": "test-financial-document.pdf",
    "sha256": hashlib.sha256(output.getvalue()).hexdigest(),
    "size_bytes": len(output.getvalue()),
}, indent=2) + "\n")
