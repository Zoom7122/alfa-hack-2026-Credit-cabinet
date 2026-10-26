#!/usr/bin/env python3
"""Build metadata only from cmd/field-catalog's synthetic collector inventory.

Usage: python3 scripts/field_catalog.py /tmp/collector-inventory.json
Paths and transformations below mirror data-collector/internal/decision/{request,statements}.go.
"""
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
SYSTEMS = {
    'CORE_BANKING': ('АБС банка', 'ИНН заявителя'), 'FNS': ('ФНС', 'ИНН; отдельные запросы зависят от типа компании и наличия руководителя'),
    'BKI': ('БКИ', 'Согласие компании; для отчёта физлица — данные руководителя и отдельное согласие'),
    'COMPLIANCE': ('Комплаенс', 'ИНН; сведения о руководителе и представителе при наличии'),
    'FSSP': ('ФССП', 'ИНН компании; данные руководителя при наличии'), 'KAD': ('Картотека арбитражных дел', 'ИНН компании'),
    'FEDRESURS': ('Федресурс', 'ИНН компании; ИНН руководителя при наличии'), 'M4D': ('Реестр МЧД', 'ИНН компании; номер доверенности при наличии'),
    'TAX_REPORTS': ('Налоговая отчётность', 'Согласие на декларации и/или сальдо ЕНС'), 'PLEDGE_REGISTRY': ('Реестр залогов', 'ИНН; VIN и серийные номера при наличии'),
    'EGRN': ('ЕГРН', 'Кадастровые номера предлагаемого залога'), 'GOSUSLUGI': ('Госуслуги', 'Данные руководителя'),
    'OPEN_BANKING': ('Другие банки', 'БИК других банков'), 'MARKETPLACES': ('Маркетплейсы', 'Токен продавца Wildberries или Ozon'),
    'OFD': ('Онлайн-кассы / ОФД', 'Токен ОФД'), 'ZAKUPKI': ('Госзакупки', 'ИНН компании'), 'SIGNALS': ('Внешние сигналы', 'ИНН компании'),
    'CBR': ('Банк России', 'Публичная ключевая ставка'), 'HH': ('hh.ru', 'Название работодателя из ФНС; вакансии после поиска работодателя'),
}
LABELS = {'amount': 'Сумма', 'status': 'Статус', 'name': 'Наименование', 'inn': 'ИНН', 'month': 'Месяц', 'date': 'Дата', 'summary': 'Сводка', 'items': 'Записи', 'client_since': 'Дата начала обслуживания в банке', 'credit_turnover': 'Кредитовый оборот', 'debit_turnover': 'Дебетовый оборот', 'avg_balance': 'Средний остаток', 'purpose': 'Назначение платежа', 'direction': 'Направление платежа', 'total_outstanding': 'Остаток задолженности', 'has_history': 'Наличие кредитной истории', 'current_overdue_days': 'Текущая просрочка, дней', 'max_overdue_days_24m': 'Максимальная просрочка за 24 месяца', 'inquiries_90d': 'Кредитные обращения за 90 дней', 'opened_at': 'Дата открытия', 'planned_close_at': 'Плановая дата погашения', 'monthly_payment': 'Ежемесячный платёж', 'key_rate': 'Ключевая ставка (XML)', 'rating': 'Рейтинг продавца', 'reviews_count': 'Количество отзывов', 'active_sku': 'Количество активных товаров', 'gross_sales': 'Валовые продажи', 'payout': 'Выплата продавцу', 'orders_count': 'Количество заказов', 'returns_count': 'Количество возвратов', 'revenue': 'Выручка', 'vacancies': 'Вакансии', 'news': 'Новости', 'tags': 'Теги', 'active_amount': 'Сумма действующих взысканий', 'bankruptcy_stage': 'Стадия банкротства', 'liquidation': 'Ликвидация', 'has_unpaid_claims': 'Неоплаченные требования', 'r': 'Дата регистрации', 'e': 'Дата прекращения деятельности', 'g': 'Руководитель из ЕГРЮЛ', 'current': 'Текущий налоговый режим', 'sulst_ex': 'Статус компании', 'invalid': 'Недостоверные сведения ЕГРЮЛ'}

def app(name):
    return [{'kind': 'APPLICATION', 'path': f'scoring.{t}.{name}'} for t in ('ip', 'legal_entity')]

def computed(*names):
    return [{'kind': 'COMPUTED', 'path': x} for x in names]

FLOW = computed('receipts_rub', 'cfads_rub', 'variable_rub', 'fixed_rub', 'historical_debt_rub', 'annual_required_rub', 'positive_cfads_months', 'previous_quarter_receipts_rub', 'recent_quarter_receipts_rub', 'history_complete')
MAPPINGS = {}

def bind(system, paths, targets):
    for path in paths:
        MAPPINGS.setdefault((system, path), []).extend(targets)

bind('CORE_BANKING', ['client.client_since'], app('bank_relationship_months'))
bind('CORE_BANKING', ['turnover.months[].month', 'turnover.months[].credit_turnover', 'turnover.months[].debit_turnover', 'transactions[].items[].date', 'transactions[].items[].amount', 'transactions[].items[].direction', 'transactions[].items[].purpose'], FLOW + app('structural_business_change') + [{'kind':'APPLICATION','path':'evidence.confirmed_receipts_rub'}, {'kind':'APPLICATION','path':'evidence.unexplained_difference_rub'}, {'kind':'APPLICATION','path':'evidence.largest_buyer_group_receipts_rub'}])
bind('CORE_BANKING', ['transactions[].items[].counterparty.inn'], [{'kind':'APPLICATION','path':'evidence.largest_buyer_group_receipts_rub'}])
bind('CORE_BANKING', ['turnover.months[].avg_balance'], app('free_cash_source_verified'))
bind('CORE_BANKING', ['loans.items'], app('debts_complete'))
bind('CORE_BANKING', ['loans.items[].current_overdue_days'], app('current_dpd_days'))
bind('CORE_BANKING', ['loans.items[].max_overdue_days_24m'], app('worst_repaid_dpd_days_24m'))
bind('CORE_BANKING', ['restrictions.card_file.has_unpaid_claims', 'restrictions.account_blocks', 'restrictions.arrests'], app('material_account_restriction'))
for subject in ('legal', 'person'):
    bind('BKI', [subject], app('bureau_received') + app('debts_complete') + app('organization_and_related_debts_complete'))
    for path, target in {'has_history':'credit_history_months','accounts[].opened_at':'credit_history_months','accounts[].current_overdue_days':'current_dpd_days','summary.inquiries_90d':'applications_90d','summary.max_overdue_days_ever':'worst_repaid_dpd_days_24m','summary.overdue_30p_24m':'worst_repaid_dpd_days_24m','summary.overdue_60p_24m':'worst_repaid_dpd_days_24m','summary.overdue_90p_24m':'worst_repaid_dpd_days_24m'}.items():
        bind('BKI', [subject+'.'+path], app(target))
    bind('BKI', [subject+'.summary.total_outstanding'], [{'kind':'APPLICATION','path':'financial_debt_rub'}])
    bind('BKI', [subject+'.accounts[].'+x for x in ('monthly_payment','planned_close_at','status')], computed('future_complete','debt_schedule_consistent'))
bind('FNS', ['egrul.rows[].r'], app('business_age_months'))
bind('FNS', ['egrul.rows[].e', 'pb.vyp.sulst_ex'], app('active') + app('liquidated_or_excluded'))
bind('FNS',['pb.vyp.pr_zd'],app('overdue_taxes_rub'))
bind('FNS', ['pb.vyp.invalid'], app('material_register_inaccuracy'))
bind('FNS', ['pb.vyp.КодОКВЭД'], app('operating_nonfinancial_company'))
bind('FNS', ['pb.liquidated'], app('liquidated_or_excluded'))
bind('FNS', ['pb.vyp.masruk[].inn'], app('identity_and_authority_verified'))
bind('FNS', ['account_blocks.*.decisions'], app('material_account_restriction'))
balance={'1100':'non_current_assets','1210':'inventories','1230':'receivables','1240':'short_term_investments','1250':'cash','1200':'current_assets','1600':'total_assets','1300':'equity','1410':'long_term_borrowings','1400':'long_term_liabilities','1510':'short_term_borrowings','1500':'current_liabilities'}
profit={'2110':'revenue','2120':'cost_of_sales','2200':'sales_profit','2330':'interest_payable','2400':'net_profit'}
for section, lines in [('balance',balance),('financialResult',profit)]:
    for raw, target in lines.items():
        targets=[{'kind':'STATEMENTS','path':period+'.'+('balance' if section=='balance' else 'profit_and_loss')+'.'+target} for period in ('current','previous')]
        if raw=='2110':targets += [{'kind':'APPLICATION','path':'evidence.confirmed_receipts_rub'},{'kind':'APPLICATION','path':'evidence.unexplained_difference_rub'},{'kind':'APPLICATION','path':'ip_economic_data.previous_comparable_receipts_rub'}]
        if raw in ('2110','1600'):targets+=app('auditor_opinion')
        bind('FNS',[f'bo_bfo[].typeCorrections[].correction.{section}.{prefix}{raw}' for prefix in ('current','previous')],targets)
bind('FNS',['bo_bfo[].period'],computed('statements_complete')+app('report_verification_level')+app('documents_complete'))
for sub in ('poa',):
    bind('M4D',[sub+'.'+k for k in ('status','representative.inn')],app('identity_and_authority_verified'))
bind('FSSP',['company.summary.active_amount'],app('material_dispute_or_enforcement'))
bind('KAD',['cases[].summary.as_defendant_active_amount'],app('material_dispute_or_enforcement'))
bind('KAD',['cases[].summary.bankruptcy_as_debtor'],app('bankrupt'))
bind('FEDRESURS',['company_messages.summary.bankruptcy_stage'],app('bankrupt'))
bind('FEDRESURS',['company_messages.summary.liquidation'],app('liquidated_or_excluded'))
bind('COMPLIANCE',['zsk.risk_level','screening.results[].listed'],app('credit_prohibited'))
bind('SIGNALS',['news.items[].tags[]'],app('key_buyer_loss'))
bind('TAX_REPORTS',['report.documents[].type','report.documents[].period','report.documents[].data.revenue_from_sales','report.documents[].data.income','report.tax_regime.current'],[{'kind':'APPLICATION','path':'evidence.confirmed_receipts_rub'},{'kind':'APPLICATION','path':'evidence.unexplained_difference_rub'},{'kind':'APPLICATION','path':'ip_economic_data.previous_comparable_receipts_rub'}]+app('income_evidence')+app('report_verification_level'))
bind('TAX_REPORTS',['report.documents[].type','report.documents[].data.debt','report.documents[].data.penalties','report.documents[].data.fines'],app('overdue_taxes_rub'))

def main():
    inventory=json.loads(pathlib.Path(sys.argv[1]).read_text())
    fields=[]
    known={(p['source']['kind'],p['source']['path']) for name in ('ip-v2','legal-v2') for p in json.loads((ROOT/'models'/f'{name}.json').read_text())['parameters']}
    for row in inventory:
        system,path=row['system'],row['path']
        leaf=path.rsplit('.',1)[-1].replace('[]','')
        row.update(id=system+':'+path,title=LABELS.get(leaf,leaf.replace('_',' ')),bindings=[b for b in MAPPINGS.get((system,path),[]) if (b['kind'],b['path']) in known])
        fields.append(row)
    target=ROOT/'internal/fieldcatalog/collector-fields.json';target.parent.mkdir(parents=True,exist_ok=True)
    target.write_text(json.dumps({'format_version':1,'systems':[{'id':k,'title':v[0],'collection_condition':v[1]} for k,v in SYSTEMS.items()],'fields':fields},ensure_ascii=False,indent=2)+'\n')
    print(f'{len(fields)} field paths across {len(SYSTEMS)} systems')

if __name__=='__main__':main()
