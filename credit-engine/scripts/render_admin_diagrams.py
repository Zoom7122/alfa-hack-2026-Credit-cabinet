"""Render the Russian admin guide as PNGs; requires Python 3 and Pillow.

Run from any directory: python credit-engine/scripts/render_admin_diagrams.py
Text and layout are kept here so the documentation images remain editable.
No services, databases, network requests or application credentials are used.
"""

from functools import lru_cache
from math import atan2, cos, sin, pi
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont, PngImagePlugin


ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs" / "diagrams"
SCALE = 2
WIDTH = 1800
C = {
    "bg": "#F4F6FA", "white": "#FFFFFF", "ink": "#17283F",
    "muted": "#52647A", "line": "#D9E1EC", "red": "#D42C38",
    "blue": "#285DB4", "blue_bg": "#EAF1FC",
    "green": "#18745B", "green_bg": "#E8F4EF",
    "amber": "#94600F", "amber_bg": "#FFF3DA",
    "purple": "#6F4BAC", "purple_bg": "#F1ECF9",
}


@lru_cache(maxsize=None)
def font(size, bold=False, mono=False):
    candidates = (
        ["C:/Windows/Fonts/consola.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"]
        if mono else
        ["C:/Windows/Fonts/segoeuib.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"]
        if bold else
        ["C:/Windows/Fonts/segoeui.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"]
    )
    for path in candidates:
        if Path(path).is_file():
            return ImageFont.truetype(path, round(size * SCALE))
    raise RuntimeError("Install Segoe UI/Consolas or DejaVu Sans/Mono with Cyrillic support")


class Diagram:
    def __init__(self, number, title, subtitle, height, sources):
        self.height = height
        self.title = title
        self.sources = sources
        self.image = Image.new("RGB", (WIDTH * SCALE, height * SCALE), C["bg"])
        self.draw = ImageDraw.Draw(self.image)
        self.rect(60, 43, 9, 100, C["red"], radius=4)
        self.text(92, 40, f"CREDIT ENGINE / АДМИНИСТРАТОР     {number:02d} / 07", 1500, 23, color=C["red"], bold=True)
        self.text(90, 82, title, 1640, 54, bold=True)
        self.text(92, 157, subtitle, 1640, 28, color=C["muted"])
        self.line([(60, height - 79), (1740, height - 79)], C["line"], 2)
        self.text(60, height - 59, sources, 1600, 21, color=C["muted"])
        self.text(1660, height - 59, f"{number:02d}/07", 80, 21, color=C["muted"])

    def rect(self, x, y, w, h, fill, outline=None, radius=18):
        box = tuple(round(v * SCALE) for v in (x, y, x + w, y + h))
        self.draw.rounded_rectangle(box, radius=radius * SCALE, fill=fill, outline=outline, width=2 * SCALE)

    def line(self, points, color=None, width=3):
        self.draw.line([(round(x * SCALE), round(y * SCALE)) for x, y in points], fill=color or C["blue"], width=width * SCALE, joint="curve")

    def arrow(self, points, color=None, width=3):
        color = color or C["blue"]
        self.line(points, color, width)
        x, y = points[-1]
        px, py = points[-2]
        angle = atan2(y - py, x - px)
        tip = [(x, y), (x - 16 * cos(angle - pi / 7), y - 16 * sin(angle - pi / 7)), (x - 16 * cos(angle + pi / 7), y - 16 * sin(angle + pi / 7))]
        self.draw.polygon([(round(a * SCALE), round(b * SCALE)) for a, b in tip], fill=color)

    def text(self, x, y, value, width, size=26, color=None, bold=False, mono=False, gap=1.27):
        face = font(size, bold, mono)
        lines = []
        for paragraph in value.split("\n"):
            current = ""
            for word in paragraph.split():
                proposed = f"{current} {word}".strip()
                if self.draw.textlength(proposed, font=face) <= width * SCALE:
                    current = proposed
                    continue
                if current:
                    lines.append(current)
                    current = ""
                # Paths and identifiers can exceed one line without spaces.
                for char in word:
                    if self.draw.textlength(current + char, font=face) > width * SCALE:
                        lines.append(current)
                        current = char
                    else:
                        current += char
            lines.append(current)
        line_h = size * gap
        for index, line in enumerate(lines):
            self.draw.text((round(x * SCALE), round((y + index * line_h) * SCALE)), line, font=face, fill=color or C["ink"], anchor="lt")
        end = y + len(lines) * line_h
        if x < 0 or x + width > WIDTH or y < 0 or end > self.height - 15:
            raise ValueError(f"Text outside content area: {value!r} at {x}, {y}")
        return end

    def card(self, x, y, w, h, title, body=(), tone="blue", size=26, title_size=32, step=None):
        self.rect(x, y, w, h, C["white"], C["line"])
        self.rect(x, y, 7, h, C[tone], radius=3)
        tx = x + 28
        cursor = y + 24
        if step is not None:
            self.rect(tx, cursor, 44, 44, C[tone + "_bg"], radius=12)
            self.text(tx + 11, cursor + 6, str(step), 32, 25, color=C[tone], bold=True)
            cursor = self.text(tx + 60, cursor + 1, title, w - 116, title_size, bold=True) + 22
        else:
            cursor = self.text(tx, cursor, title, w - 56, title_size, bold=True) + 14
        for paragraph in body:
            cursor = self.text(tx, cursor, paragraph, w - 56, size, color=C["muted"]) + 11
        if cursor - 11 > y + h - 18:
            raise ValueError(f"Card overflow: {title!r} needs {cursor - y:.0f}px, has {h}px")
        return cursor

    def note(self, x, y, w, h, title, body, tone="amber", size=26):
        self.rect(x, y, w, h, C[tone + "_bg"])
        end = self.text(x + 25, y + 20, title, w - 50, 28, bold=True, color=C[tone]) + 9
        end = self.text(x + 25, end, body, w - 50, size, color=C["ink"])
        if end > y + h - 15:
            raise ValueError(f"Note overflow: {title!r} needs {end - y + 15:.0f}px, has {h}px")

    def code_card(self, x, y, w, h, title, commands, body="", step=None, size=23):
        end = self.card(x, y, w, h, title, step=step, title_size=29)
        end += 2
        end = self.text(x + 28, end, commands, w - 56, size, mono=True, gap=1.28)
        if body:
            end = self.text(x + 28, end + 14, body, w - 56, 24, color=C["muted"])
        if end > y + h - 15:
            raise ValueError(f"Command card overflow: {title!r} needs {end - y + 15:.0f}px, has {h}px")

    def save(self, name):
        OUT.mkdir(parents=True, exist_ok=True)
        path = OUT / name
        info = PngImagePlugin.PngInfo()
        info.add_text("Title", self.title)
        info.add_text("Sources", self.sources)
        info.add_text("Generator", "credit-engine/scripts/render_admin_diagrams.py")
        result = self.image.resize((WIDTH, self.height), Image.Resampling.LANCZOS)
        result.save(path, pnginfo=info, optimize=True, dpi=(144, 144))
        print(f"{path.relative_to(ROOT)}: {WIDTH} x {self.height}")


def architecture():
    d = Diagram(1, "Как устроена админка", "МС3 — отдельный сервис: редактор моделей, проверка, выпуск и просмотр событий банка.", 1240,
                "Код: internal/app/admin.go, internal/adminui, internal/adminstore • Документы: 24, 26–28, 32")
    d.card(60, 235, 400, 185, "Браузер", ["Логин и пароль", "Настольный интерфейс"], size=27)
    d.card(560, 235, 650, 285, "МС3 · admin-service :8084", ["Встроенные HTML / CSS / JavaScript", "Gin API: /api/admin/v1/*", "Общие проверка модели, симулятор и генератор комплекта"], size=28)
    d.card(1330, 235, 410, 285, "PostgreSQL", ["Схема credit_engine_admin", "Черновики и ревизии, модели, ZIP-релизы, события и сессии"], tone="green", size=27)
    d.arrow([(460, 326), (560, 326)])
    d.arrow([(1210, 326), (1330, 326)], C["green"])
    d.text(1232, 285, "SQL", 90, 23, color=C["green"], bold=True)
    d.card(60, 465, 400, 500, "Два вида доступа", ["Администратор: сессия в cookie; изменения защищены Origin и CSRF.", "МС2: отдельный Bearer-токен ADMIN_EVENTS_TOKEN.", "Интерфейс встроен в бинарник; отдельная сборка фронтенда не нужна."], size=27, title_size=31)
    d.card(560, 725, 650, 240, "МС1 · scoring-service :8082", ["Оценка и подбор условий", "Каталог встроен при сборке. GET /api/v1/models: версии и SHA-256."], size=27)
    d.card(1330, 725, 410, 240, "МС2 · банк :8083", ["Проверка и учёт выдачи", "Единственный отправитель кредитных событий в МС3"], size=27, title_size=29)
    d.arrow([(760, 520), (760, 725)])
    d.rect(545, 570, 445, 90, C["bg"], radius=8)
    d.text(575, 579, "Читает метаданные МС1", 410, 26, bold=True, color=C["blue"])
    d.text(575, 617, "ADMIN_SCORING_URL", 410, 23, mono=True, color=C["muted"])
    d.arrow([(1210, 846), (1330, 846)])
    d.arrow([(1535, 725), (1535, 597), (1125, 597), (1125, 520)], C["purple"])
    d.rect(1165, 555, 545, 76, C["bg"], radius=8)
    d.text(1185, 561, "МС2 → МС3: события", 510, 25, bold=True, color=C["purple"])
    d.text(1185, 600, "POST /api/v1/credit-events", 510, 23, mono=True, color=C["muted"])
    d.note(60, 1010, 1680, 150, "Рабочая модель меняется через выпуск, сборку и развёртывание МС1", "Сохранённый черновик и подготовленный ZIP остаются в МС3. Симуляция выполняет расчёт модели без обращения к банку и поставщикам.")
    d.save("07_admin_architecture.png")


def startup():
    d = Diagram(2, "Как включить админку", "Linux + Docker / Compose. Все команды ниже выполняются из каталога credit-engine/.", 1880,
                "Источники: compose.credit-engine.yaml, config/admin.env.example, cmd/admin-password • Документы: 24, 33")
    d.note(60, 225, 1680, 155, "Сначала подготовьте внешние зависимости", "Нужны существующая Docker-сеть, PostgreSQL с правом создания схемы и поставщик отчётности Б. FACTS_URL нужен моделям с новыми фактами. Корневой Compose админку не запускает.", size=27)
    d.rect(60, 395, 720, 1070, C["white"], C["line"])
    d.text(90, 425, "Что заполнить в .env.admin", 650, 34, bold=True)
    settings = [
        ("CREDIT_ENGINE_NETWORK", "Имя уже существующей Docker-сети."),
        ("ADMIN_DATABASE_URL", "Адрес PostgreSQL, доступный из контейнера."),
        ("ADMIN_USERNAME / ADMIN_PASSWORD_HASH", "Логин и bcrypt-хеш пароля. Хеш с $ заключить в одинарные кавычки."),
        ("ADMIN_EVENTS_TOKEN", "Одинаковый секрет МС2/МС3: 32–256 печатных ASCII-символов без пробелов."),
        ("STATEMENTS_URL / FACTS_URL", "Сетевые адреса поставщиков. localhost внутри контейнера указывает на сам контейнер."),
        ("ADMIN_PUBLIC_ORIGIN", "Для локального входа: http://localhost:8084 — точно тот же адрес, что в браузере."),
        ("ADMIN_COOKIE_SECURE", "false для локального HTTP; true при HTTPS с соответствующим Origin."),
    ]
    y = 495
    for key, value in settings:
        y = d.text(90, y, key, 650, 24, mono=True, color=C["blue"]) + 10
        y = d.text(90, y, value, 650, 26, color=C["muted"]) + 29
    assert y < 1448, "Settings overflow"
    d.code_card(825, 395, 915, 135, "Собрать образ", "docker build -t credit-engine:local .", step=1)
    d.code_card(825, 550, 915, 180, "Создать локальный файл настроек", "cp config/admin.env.example .env.admin", "Заполните значения слева. Файл содержит секреты.", step=2)
    d.code_card(825, 745, 915, 285, "Получить хеш пароля (Bash)", "read -rs ADMIN_PASSWORD\nprintf '%s' \"$ADMIN_PASSWORD\" | docker run --rm -i \\\n  --entrypoint /admin-password credit-engine:local\nunset ADMIN_PASSWORD", "Вставьте выведенный хеш в ADMIN_PASSWORD_HASH.", step=3, size=23)
    d.code_card(825, 1050, 915, 190, "Применить миграции до запуска", "docker compose --env-file .env.admin \\\n  -f compose.credit-engine.yaml \\\n  run --rm admin-migrate up", step=4)
    d.code_card(825, 1260, 915, 205, "Запустить три сервиса", "docker compose --env-file .env.admin \\\n  -f compose.credit-engine.yaml up -d", "Порты примера доступны только через 127.0.0.1.", step=5)
    d.rect(60, 1500, 1680, 245, C["green_bg"])
    d.text(90, 1527, "6   Проверить процесс и войти", 1550, 34, bold=True, color=C["green"])
    d.text(90, 1590, "curl http://localhost:8084/healthz\ncurl http://localhost:8082/healthz", 760, 25, mono=True)
    d.text(920, 1590, "Откройте http://localhost:8084\nВойдите своим логином и паролем.", 770, 28, bold=True)
    d.text(90, 1690, "/healthz проверяет процесс. Вход, доступ к БД и сведения МС1 проверяйте отдельно.", 1550, 25, color=C["muted"])
    d.save("08_admin_startup.png")


def navigation():
    d = Diagram(3, "Куда идти в интерфейсе", "После входа доступны пять разделов. Это карта действий по текущему интерфейсу.", 1290,
                "Источники: internal/adminui/web/app.js, workbench.js, audit.js • Документы: 28, 31, 32, 35")
    rows = [
        ("Модели", "Выбрать ИП / ЮЛ и опубликованную версию. Открыть состав и скопировать в черновик.", "Неизменяемая основа", "blue"),
        ("Черновики", "Редактировать текущую ревизию, сохранять изменения с основанием, читать историю.", "Рабочие изменения", "purple"),
        ("Проверка и выпуск", "Запустить симуляцию, сравнить версии, проверить документы и подготовить ZIP.", "Проверенный комплект", "amber"),
        ("Релизы", "Скачать подготовленный комплект. Отдельно сверить версии и SHA-256 работающего МС1.", "Подготовлено / запущено", "green"),
        ("Сообщения", "Найти сохранённое событие банка по фильтрам и открыть подробности.", "Просмотр событий МС2", "blue"),
    ]
    for index, (title, action, outcome, tone) in enumerate(rows):
        y = 235 + index * 162
        d.rect(60, y, 1680, 140, C["white"], C["line"])
        d.rect(85, y + 26, 65, 65, C[tone + "_bg"], radius=17)
        d.text(100, y + 37, f"{index + 1:02d}", 55, 29, bold=True, color=C[tone])
        d.text(177, y + 31, title, 335, 31, bold=True)
        d.line([(535, y + 22), (535, y + 118)], C["line"], 2)
        d.text(565, y + 26, action, 755, 27, color=C["muted"])
        d.rect(1370, y + 25, 340, 91, C[tone + "_bg"], radius=12)
        d.text(1390, y + 42, outcome, 300, 26, bold=True, color=C[tone])
    d.note(60, 1070, 1680, 120, "Вход: одна административная учётная запись", "Кнопка «Выйти» завершает сессию на сервере. При истечении сессии потребуется повторный вход.", tone="blue", size=25)
    d.save("09_admin_navigation.png")


def editing():
    d = Diagram(4, "Как изменить модель", "Опубликованная модель и исторические ревизии доступны для чтения; редактируется текущий черновик.", 1570,
                "Источники: internal/adminui/web/editor.js, model-edit.js, rules-editor.js • Документы: 29, 30, 35")
    cards = [
        (60, "Выбрать исходную модель", ["«Модели» → ИП / ЮЛ → версия → «Копировать в черновик»."], "blue"),
        (650, "Создать черновик", ["Задать новую версию и основание изменения. Пустую модель тоже можно создать."], "purple"),
        (1240, "Открыть текущую ревизию", ["Проверить номер ревизии, сводку максимумов и ошибки модели."], "purple"),
    ]
    for x, title, body, tone in cards:
        d.card(x, 235, 500, 205, title, body, tone, size=26, title_size=30)
    d.arrow([(560, 338), (650, 338)])
    d.arrow([(1150, 338), (1240, 338)])
    d.card(60, 485, 540, 290, "Параметры", ["Активность, обязательность, условия и зависимости.", "Новый факт поставщика: число / boolean, JSON-имя, тип, единица и версия схемы."], tone="purple", size=26)
    d.card(630, 485, 540, 290, "Критерии и баллы", ["Блок 750 / 250, максимум, варианты ответа и балльная шкала.", "Условия применения и поведение при неизвестных значениях."], tone="purple", size=26)
    d.card(1200, 485, 540, 290, "Проверки данных", ["Достаточность и применимость.", "Пути недостающих данных и сообщение для NEED_INFO."], tone="purple", size=26)
    d.card(60, 805, 825, 215, "Стопы и рассмотрение", ["Типизированные условия, причины, необходимые проверки и критерии. Стоп подтверждается только при TRUE."], tone="purple", size=26)
    d.card(915, 805, 825, 215, "Классы D / C / B / A", ["Целые включительные границы 0–1000 без разрывов и пересечений. Для C / D задаются причины."], tone="purple", size=26)
    d.card(60, 1060, 500, 205, "Применить к черновику", ["Изменения остаются в текущем окне браузера."], tone="blue", size=27, title_size=30)
    d.card(650, 1060, 500, 205, "Сохранить новую ревизию", ["Указать основание. Отправить определение на сервер."], tone="green", size=27, title_size=30)
    d.card(1240, 1060, 500, 205, "Перейти к проверке", ["Симуляция использует сохранённую точную ревизию."], tone="green", size=27, title_size=30)
    d.arrow([(560, 1160), (650, 1160)], C["green"])
    d.arrow([(1150, 1160), (1240, 1160)], C["green"])
    d.note(60, 1305, 825, 150, "Максимумы каждой модели: 750 + 250 = 1000", "Промежуточный черновик можно сохранить с ошибками. Симуляция и выпуск требуют общей проверки.", size=25)
    d.note(915, 1305, 825, 150, "Если возник конфликт ревизий", "Скачайте локальное определение, откройте актуальную ревизию и согласуйте изменения перед сохранением.", size=25)
    d.save("10_admin_model_editing.png")


def simulation():
    d = Diagram(5, "Как проверить изменения", "Раздел «Проверка и выпуск» считает сохранённую модель на самостоятельном тестовом наборе.", 1470,
                "Источники: internal/adminui/web/workbench.js, internal/simulation • Документы: 27, 31, 35")
    d.rect(60, 235, 670, 740, C["white"], C["line"])
    d.text(90, 265, "JSON: все пять ключей обязательны", 610, 31, bold=True)
    fields = [
        ("evaluation_date", "Явная дата оценки YYYY-MM-DD."),
        ("application", "Заявка по контракту МС1."),
        ("statements", "Отчётность ЮЛ либо null."),
        ("facts_date", "Дата дополнительных фактов либо null."),
        ("additional_facts", "Именованные number / boolean / null; объект либо null."),
    ]
    y = 335
    for key, value in fields:
        y = d.text(90, y, key, 610, 26, mono=True, color=C["blue"]) + 8
        y = d.text(90, y, value, 610, 25, color=C["muted"]) + 16
    assert y < 830, "Simulation fields overlap examples"
    d.text(90, 840, "Готовые примеры:", 610, 26, bold=True)
    d.text(90, 888, "testdata/simulation/ip-v2.json\ntestdata/simulation/le-v2.json", 610, 24, mono=True)
    d.card(810, 235, 930, 185, "Выбрать версию / ревизию", ["Справа — новая модель. Слева — исходная версия для сравнения. Сначала сохраните изменения редактора."], step=1, title_size=30, size=27)
    d.arrow([(1275, 420), (1275, 455)])
    d.card(810, 455, 930, 185, "Загрузить JSON и выполнить расчёт", ["Данные поставщиков задаются в наборе. Один набор и одна дата используются для обеих моделей."], step=2, title_size=30, size=27)
    d.arrow([(1275, 640), (1275, 675)])
    d.card(810, 675, 930, 300, "Изучить результат", ["Маршрут, класс и итоговый балл при наличии; причины и недостающие поля.", "Критерии: значение, баллы, участие в итоге. Параметры: исходные и эффективные значения. Проверки и правила: статусы и применение."], step=3, title_size=30, size=27)
    d.note(60, 1015, 1680, 170, "«Сравнить расчёты» и «Показать изменения модели»", "Сравнение расчётов показывает изменения маршрута, класса и баллов на одном наборе. Просмотр изменений определения показывает SHA и изменённые поля; тестовый набор для него не нужен.", tone="blue", size=28)
    d.note(60, 1220, 1680, 150, "SELECT_OFFER означает готовность к подбору условий", "Симуляция не обращается к банку, оптимизатору и поставщикам. При score = null диагностические баллы не становятся итогом; null отличается от 0 и false.", size=27)
    d.save("11_admin_simulation.png")


def release():
    d = Diagram(6, "Как выпустить и применить модель", "Подготовка комплекта в админке и развёртывание рабочей модели — отдельные этапы.", 1570,
                "Источники: internal/adminui/web/workbench.js, internal/release, Dockerfile • Документы: 21, 31, 36")
    d.text(60, 235, "В АДМИНКЕ  /  «Проверка и выпуск»", 1680, 26, bold=True, color=C["purple"])
    top = [
        (60, "Полный состав", ["Новая версия релиза, базовый релиз, описание изменений.", "Включить нужные старые версии и точные ревизии черновиков."], 1),
        (495, "Предпросмотр", ["«Проверить и показать документы».", "Исправить ошибки, проверить 750 / 250 / 1000, OpenAPI, таблицы и manifest."], 2),
        (930, "Подготовка", ["Указать основание → «Подготовить релиз».", "После изменения состава повторить предпросмотр. Комплект неизменяем."], 3),
        (1365, "ZIP + SHA-256", ["«Скачать комплект».", "Сохранить архив, manifest и контрольные суммы. Передать ответственному за сборку."], 4),
    ]
    for x, title, body, step in top:
        d.card(x, 290, 375, 370, title, body, tone="purple", size=26, title_size=29, step=step)
    for x in [435, 870, 1305]:
        d.arrow([(x, 475), (x + 60, 475)], C["purple"])
    d.note(60, 700, 1680, 150, "ZIP подготовлен; работающий МС1 продолжает использовать встроенный каталог", "Новая схема фактов должна быть согласована и поддержана поставщиком до включения новой модели.", size=28)
    d.text(60, 875, "В ИСХОДНИКАХ И СТЕКЕ  /  ответственные за сборку и развёртывание", 1680, 26, bold=True, color=C["blue"])
    d.card(60, 930, 500, 330, "Проверить и перенести", ["Сверить SHA-256 архива и файлов, manifest и изменения.", "Перенести согласованные определения, Go / OpenAPI / Markdown и metadata в credit-engine."], size=26, title_size=29, step=5)
    d.card(650, 930, 500, 330, "Проверить и собрать", ["modelgen validate / check / archive для того же состава.", "Пройти приёмку и собрать Linux / Docker-образ с новым каталогом."], size=26, title_size=29, step=6)
    d.card(1240, 930, 500, 330, "Развернуть и сверить", ["Развернуть совместимый комплект. Проверить вход и /healthz.", "«Релизы»: обновить сведения МС1 и сравнить фактические версии и SHA-256."], tone="green", size=26, title_size=29, step=7)
    d.arrow([(560, 1085), (650, 1085)])
    d.arrow([(1150, 1085), (1240, 1085)], C["green"])
    d.note(60, 1300, 1680, 150, "После проверки участник А может выбирать новую model_version", "Без версии прежние запросы используют v2. Неизвестная версия не заменяется новой. Для повторов старых заявок сохраните прежние версии в каталоге.", tone="green", size=27)
    d.save("12_admin_release.png")


def messages():
    d = Diagram(7, "Как читать сообщения банка", "Раздел «Сообщения» показывает сохранённые события МС2 и их подробности.", 1440,
                "Источники: internal/events, internal/httpapi/admin_events.go, internal/adminui/web/audit.js • Документы: 26, 32")
    d.card(60, 235, 500, 220, "МС2 отправляет событие", ["POST /api/v1/credit-events", "Bearer ADMIN_EVENTS_TOKEN"], size=26, title_size=31)
    d.card(650, 235, 500, 220, "МС3 сохраняет в PostgreSQL", ["Проверка контракта → запись → commit → подтверждение 202."], tone="green", size=27, title_size=31)
    d.card(1240, 235, 500, 220, "Событие видно в списке", ["Найдите запись по фильтрам и откройте карточку."], size=27, title_size=31)
    d.arrow([(560, 345), (650, 345)])
    d.arrow([(1150, 345), (1240, 345)], C["green"])
    d.note(60, 495, 1680, 150, "Если событие ещё не пришло: проверяйте PENDING в ответе МС2", "Ошибка доставки не отменяет уже учтённую выдачу. Идентичный повтор операции в МС2 повторяет недоставленное событие без второй выдачи; сохранённый одинаковый event_id не создаёт дубль.", size=27)
    d.rect(60, 685, 650, 480, C["white"], C["line"])
    d.text(90, 715, "Три типа событий", 590, 32, bold=True)
    event_types = [
        ("LOAN_ISSUED", "Учебная выдача учтена. loan_id, условия и результат подбора."),
        ("BORROWER_REVIEW", "Рассмотрение заёмщика: причины, без выбранной сделки."),
        ("BANK_REVIEW", "Банковские ограничения: условия и список проблем."),
    ]
    y = 785
    for name, meaning in event_types:
        y = d.text(90, y, name, 590, 26, mono=True, color=C["blue"]) + 10
        y = d.text(90, y, meaning, 590, 26, color=C["muted"]) + 14
    assert y < 1158, "Event types overflow"
    d.card(760, 685, 980, 480, "Действия администратора", ["1. Отфильтровать: тип события, тип / ID заёмщика, ID заявки, версия модели, период времени.", "2. Открыть карточку: сверить ID, время события и приёма, SHA-256, условия, критерии, причины и банковские проблемы.", "3. Читать значения с учётом типа: false, 0 и null различаются. События доступны только для просмотра."], size=28, title_size=32)
    d.note(60, 1205, 1680, 150, "Это журнал доставленных событий банка", "Ранние отказы и NEED_INFO МС1 сюда не попадают. Команды одобрения заявки или изменения события в админке не предусмотрены.", size=27)
    d.save("13_admin_messages.png")


if __name__ == "__main__":
    architecture()
    startup()
    navigation()
    editing()
    simulation()
    release()
    messages()
