package orders

const bootstrapSchema = `
CREATE TABLE IF NOT EXISTS service_orders (
	id BIGSERIAL PRIMARY KEY,
	code TEXT NOT NULL UNIQUE,
	customer_name TEXT NOT NULL,
	event_name TEXT NOT NULL,
	event_city TEXT NOT NULL,
	event_date DATE NOT NULL,
	status TEXT NOT NULL CHECK (
		status IN (
			'orcamento',
			'agendado',
			'em_execucao',
			'aguardando_retorno',
			'finalizado'
		)
	),
	crew_size INTEGER NOT NULL DEFAULT 1 CHECK (crew_size > 0),
	vehicle_label TEXT NOT NULL,
	inventory_summary TEXT NOT NULL DEFAULT '',
	total_amount NUMERIC(12, 2) NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
	balance_due NUMERIC(12, 2) NOT NULL DEFAULT 0 CHECK (balance_due >= 0),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE service_orders ADD COLUMN IF NOT EXISTS install_date DATE;
ALTER TABLE service_orders ADD COLUMN IF NOT EXISTS return_date DATE;
ALTER TABLE service_orders ALTER COLUMN inventory_summary SET DEFAULT '';

CREATE TABLE IF NOT EXISTS service_order_items (
	id BIGSERIAL PRIMARY KEY,
	service_order_id BIGINT NOT NULL REFERENCES service_orders(id) ON DELETE CASCADE,
	equipment_id BIGINT NOT NULL,
	quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_service_order_items_so ON service_order_items(service_order_id);

CREATE TABLE IF NOT EXISTS service_order_checklist (
	id BIGSERIAL PRIMARY KEY,
	service_order_id BIGINT NOT NULL REFERENCES service_orders(id) ON DELETE CASCADE,
	equipment_id BIGINT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('ok','avariado','perdido','nao_retornado')),
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (service_order_id, equipment_id)
);

CREATE INDEX IF NOT EXISTS idx_service_order_checklist_so ON service_order_checklist(service_order_id);

CREATE TABLE IF NOT EXISTS service_order_charges (
	id BIGSERIAL PRIMARY KEY,
	service_order_id BIGINT NOT NULL REFERENCES service_orders(id) ON DELETE CASCADE,
	equipment_id BIGINT,
	charge_type TEXT NOT NULL CHECK (charge_type IN ('avaria','perda','despesa','multa')),
	amount NUMERIC(12, 2) NOT NULL DEFAULT 0 CHECK (amount >= 0),
	description TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_service_order_charges_so ON service_order_charges(service_order_id);

CREATE TABLE IF NOT EXISTS "CadClass" (
	"DescClass" TEXT
);

CREATE TABLE IF NOT EXISTS "CadDepto" (
	"IdDepto" BIGINT PRIMARY KEY,
	"Depto" TEXT
);

CREATE TABLE IF NOT EXISTS "CadFunc" (
	"Idfunc" BIGINT PRIMARY KEY,
	"NomeFunc" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "CadLocacao" (
	"IdLoc" BIGINT PRIMARY KEY,
	"DescLoc" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "CadSetor" (
	"IdSetor" BIGINT PRIMARY KEY,
	"Setor" TEXT
);

CREATE TABLE IF NOT EXISTS "CadUF" (
	"Uf" TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS "CadUsuario" (
	"Idus" BIGINT PRIMARY KEY,
	"Nome" TEXT NOT NULL,
	"Senha" TEXT NOT NULL,
	"DataDesl" DATE,
	"IdSetorExt" BIGINT REFERENCES "CadSetor"("IdSetor")
);

CREATE TABLE IF NOT EXISTS "TabCidade" (
	"Cidade" TEXT NOT NULL,
	"Uf" TEXT NOT NULL,
	PRIMARY KEY ("Cidade", "Uf")
);

CREATE TABLE IF NOT EXISTS "TabClientes" (
	"IdCliente" BIGINT PRIMARY KEY,
	"RazCliente" TEXT NOT NULL,
	"End" TEXT,
	"nr" TEXT,
	"Compl" TEXT,
	"Bairro" TEXT,
	"Cidade" TEXT,
	"TelFixo" TEXT,
	"TelCel" TEXT,
	"ContatoCliente" TEXT,
	"Cnpj" TEXT,
	"Cpf" TEXT,
	"CNPJ_CPF" TEXT,
	"Tipo" TEXT,
	"Motivo" TEXT
);

CREATE TABLE IF NOT EXISTS "TabMotCanc" (
	"IdMotCanc" BIGINT PRIMARY KEY,
	"DescMotivo" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "TabVeiculo" (
	"IdVeic" BIGINT PRIMARY KEY,
	"DescVeic" TEXT NOT NULL,
	"MarcaVeic" TEXT,
	"Placa" TEXT
);

CREATE TABLE IF NOT EXISTS "TabEquip" (
	"IdEquip" BIGINT PRIMARY KEY,
	"DescEquip" TEXT NOT NULL,
	"Marca" TEXT,
	"SemUso" BOOLEAN NOT NULL DEFAULT FALSE,
	"Vlr" NUMERIC(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS "TabServico" (
	"IdServ" BIGINT PRIMARY KEY,
	"DescServ" TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "TabOrc" (
	"IdOrc" BIGINT PRIMARY KEY,
	"IdCliExt" BIGINT REFERENCES "TabClientes"("IdCliente"),
	"DtEmis" DATE,
	"DtAprov" DATE,
	"DtReprov" DATE,
	"PzEnt" TEXT,
	"ValProsta" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"Aprov" BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS "TabItemOrc" (
	"IdItemOrc" BIGINT PRIMARY KEY,
	"IdOrcExt" BIGINT REFERENCES "TabOrc"("IdOrc"),
	"DescItem" TEXT NOT NULL,
	"NrItem" TEXT,
	"Qdade" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"Unid" TEXT,
	"VlrUnit" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"VlrTotal" NUMERIC(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS "TabItemExcOrc" (
	"IdItemOrcExt" BIGINT REFERENCES "TabItemOrc"("IdItemOrc"),
	"IdOrcExtbx" BIGINT REFERENCES "TabOrc"("IdOrc"),
	"DtExec" DATE,
	"NrNF" TEXT,
	"NrOs" TEXT,
	"QdadeBx" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"St" TEXT
);

CREATE TABLE IF NOT EXISTS "TabAgenda" (
	"IdServ" BIGINT PRIMARY KEY,
	"IdClient" BIGINT REFERENCES "TabClientes"("IdCliente"),
	"TipEvent" TEXT,
	"Status" TEXT,
	"StAberto" BOOLEAN NOT NULL DEFAULT FALSE,
	"StInst" BOOLEAN NOT NULL DEFAULT FALSE,
	"StConc" BOOLEAN NOT NULL DEFAULT FALSE,
	"CancEvent" BOOLEAN NOT NULL DEFAULT FALSE,
	"DtEvent" DATE,
	"HrEvent" TIME,
	"datInst" DATE,
	"HrInst" TIME,
	"dtret" DATE,
	"DtAprov" DATE,
	"NrAprov" TEXT,
	"QuemAprov" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"QuemCanc" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"QuemNeg" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"IdFuncInst" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"IdFunciRet" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"MotCanc" BIGINT REFERENCES "TabMotCanc"("IdMotCanc"),
	"DescEvento" TEXT,
	"FormPag" TEXT,
	"EndEvent" TEXT,
	"NrEnd" TEXT,
	"ComplEnd" TEXT,
	"Bairro" TEXT,
	"Cidade" TEXT,
	"TelContEvent" TEXT,
	"Tel2ContEvent" TEXT,
	"ContEvent" TEXT,
	"Cont2Event" TEXT,
	"Obsagend" TEXT
);

CREATE TABLE IF NOT EXISTS "TabAgendaItens" (
	"IdItensAgend" BIGINT PRIMARY KEY,
	"IdAgendaExt" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"IdItemExt" BIGINT REFERENCES "TabEquip"("IdEquip"),
	"Desc" TEXT NOT NULL,
	"NrServ" TEXT,
	"Qdade" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"Unid" TEXT,
	"VlrUnit" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"VlrTotal" NUMERIC(12, 2) NOT NULL DEFAULT 0,
	"ObsItem" TEXT
);

CREATE TABLE IF NOT EXISTS "TabEquipSaida" (
	"IdAgendExt" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"IdItemExt" BIGINT REFERENCES "TabEquip"("IdEquip"),
	"QdaSaida" NUMERIC(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS "TabEscala" (
	"IdEscala" BIGINT PRIMARY KEY,
	"IdFuncExt" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"IdServExt" BIGINT REFERENCES "TabAgenda"("IdServ")
);

CREATE TABLE IF NOT EXISTS "TabEscalaRet" (
	"IdEscala" BIGINT PRIMARY KEY,
	"IdFuncExt" BIGINT REFERENCES "CadFunc"("Idfunc"),
	"IdServExt" BIGINT REFERENCES "TabAgenda"("IdServ")
);

CREATE TABLE IF NOT EXISTS "TabFotos" (
	"IdFotos" BIGINT PRIMARY KEY,
	"IdEvent" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"Foto" BYTEA
);

CREATE TABLE IF NOT EXISTS "TabKit" (
	"IdKit" BIGINT PRIMARY KEY,
	"IdItemEquipExt" BIGINT REFERENCES "TabEquip"("IdEquip"),
	"IdItemLocExt" BIGINT REFERENCES "CadLocacao"("IdLoc"),
	"QdeItem" NUMERIC(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS "TabPgto" (
	"IdAgendExt" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"NrDocRec" TEXT,
	"DocRec" TEXT,
	"FormRec" TEXT
);

CREATE TABLE IF NOT EXISTS "TabSaidaVeic" (
	"IdSaidVeic" BIGINT PRIMARY KEY,
	"IdEvento" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"IdVeicExt" BIGINT REFERENCES "TabVeiculo"("IdVeic")
);

CREATE TABLE IF NOT EXISTS "TabSaidaVeicRet" (
	"IdSaidVeic" BIGINT PRIMARY KEY,
	"IdEvento" BIGINT REFERENCES "TabAgenda"("IdServ"),
	"IdVeicExt" BIGINT REFERENCES "TabVeiculo"("IdVeic")
);
`
