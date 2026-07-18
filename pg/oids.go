package pg

// postgres builtin type OIDs we use when constructing RowDescription.
// God's own types straight out of pg_type; names chosen to mirror oid.T_*.
const (
	OIDOID      = 26
	BoolOID     = 16
	ByteaOID    = 17
	CharOID     = 18
	NameOID     = 19
	Int8OID     = 20
	Int2OID     = 21
	Int4OID     = 23
	TextOID     = 25
	OIDvOID     = 30
	Int8v       = 1016 // not used
	JSONOID     = 114
	Float4OID   = 700
	Float8OID   = 701
	VarcharOID  = 1043
	RegclassOID = 2205
	RecordOID   = 2249
)

// type kind: a hint for the format code we send.
type pgType uint32

const (
	Text  pgType = TextOID
	Name  pgType = NameOID
	Int4  pgType = Int4OID
	Int8  pgType = Int8OID
	OID   pgType = OIDOID
	Bool  pgType = BoolOID
	Bytea pgType = ByteaOID
)

// colOID returns the DataTypeOID for a value kind, used in RowDescription.
func colOID(t pgType) uint32 { return uint32(t) }
