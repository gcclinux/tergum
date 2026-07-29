# Graph Report - .  (2026-07-28)

## Corpus Check
- 236 files · ~237,794 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 2685 nodes · 6758 edges · 163 communities (139 shown, 24 thin omitted)
- Extraction: 83% EXTRACTED · 17% INFERRED · 0% AMBIGUOUS · INFERRED: 1173 edges (avg confidence: 0.79)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Alpine.js Frontend Bundle
- Crypto and Chunk Processing
- HTMX Frontend Bundle
- Deletion Command Logic
- Client Registry
- gRPC Client and Heartbeat
- Service Management CLI
- Auth and Migration
- Database Repository
- gRPC Mock Repository
- Observability and Metrics
- Configuration Management
- Backup Scheduler
- WebUI API Handlers
- WebUI Navigation and Server
- Backup Trigger WebUI
- Backup Entry Model
- Connection Factory
- gRPC Error Handling
- WebUI Asset Serving
- gRPC Proto Service Registration
- Database Repository Tests
- Server Core
- TLS Certificate Management
- WebUI Server Integration
- Chunked Stream Processing
- Backup Scanner
- Command Service Proto
- Remote Client Connector
- Certificate Bootstrap
- Restore Engine Tests
- Data Stream Proto
- Restore Engine
- Backup Command CLI
- File Watcher Core
- Data Service gRPC
- Data Service Mocks
- Ongoing Backup Scheduler
- Alpine.js Internals A
- Backup Engine Core
- TLS Certificate Generation
- gRPC Tunnel Hub
- Command Server Operations
- Domain Error Types
- Retention Engine Tests
- CAS Dedup Tests
- Alpine.js Internals B
- Alpine.js/HTMX Internals
- Backup Manifest
- Proto Codec and Versioning
- File Watcher Prop Tests
- Restore Bug Prop Tests
- Command Tunnel Operations
- Data Server Core
- Data Server Mocks
- Ongoing Backup Tests
- Root Command CLI
- Client Watcher Commands
- Command Server API
- Backup Engine Tests
- WebUI API Core
- Watch Command CLI
- Job and Policy Persistence
- Sync Database Mock Stream
- WebUI Template Handlers
- Scanner Platform Support
- Remote Data Source
- Content-Addressable Storage
- WebUI Sort Logic
- Encryption Architecture
- Client Command CLI
- Delete Command CLI
- Server Infrastructure Components
- Client Command Server
- Retention Dry-Run Tests
- Retention Engine Core
- Mock Watcher Tests
- Dashboard Cards API
- WebUI Paths Handler
- Watcher Controller WebUI
- Frontend Tech Stack
- Paths Command CLI
- Dashboard Metrics Display
- Push Restore and Tunnel
- gRPC Client Prop Tests
- Integration Tests
- WebUI Metrics Handler
- WebUI Toast Notifications
- WebUI Toast and Watchers
- NAT and Cross-Client Features
- Restore Command CLI
- BLAKE3 Hash Functions
- Server Tests
- Navigation Tests
- Database Write Queue
- Tunnel Client
- Retention Prop Tests
- Alpine.js Effects
- Config Settings WebUI
- Path Validator
- Admin Command CLI
- Status Command CLI
- Config Settings Display
- Node Command CLI
- Retention Concepts
- Client Backup Trigger
- Backup Stop API Tests
- Staleness Detection Tests
- Blocking Server Connection
- Build Scripts Shell
- Service Unix Detach
- Service Windows Detach
- Remote Restore Features
- Integration Test Helpers
- Retention Deletion Adapter
- Staleness Polling State
- System Stats Linux
- System Stats Windows
- Backup Result Mocks
- List Command CLI
- File Watcher Architecture
- Ping Commands
- CAS BLAKE3 Prop Tests
- Node Settings WebUI
- System Stats macOS
- Cleanup Command CLI
- Stop Command CLI
- Get Status RPC
- Env File Loader
- WebUI Handlers Helpers
- Navigation Prop Tests
- Shell Template Tests
- Staleness Prop Tests
- UI Shell Components
- Version Bump Script
- Backup Job Model
- Metrics Server Component
- Deduplication Feature
- Ongoing Backup Feature
- File Metadata Model
- v2 to v3 Migration
- Node Role Concept
- Go Module Package
- Dark Theme UI
- Data Card Partial
- Layout Template
- Metrics Navigation
- Skeleton Loader
- Sortable Table Header
- Theme Toggle UI
- Watchers Navigation

## God Nodes (most connected - your core abstractions)
1. `Join()` - 172 edges
2. `SQLiteRepository` - 55 edges
3. `BackupEntry` - 51 edges
4. `NewRepository()` - 46 edges
5. `NewEncryptor()` - 44 edges
6. `mockRepository` - 41 edges
7. `Server` - 41 edges
8. `printOutput()` - 40 edges
9. `mockRepo` - 39 edges
10. `Registry` - 37 edges

## Surprising Connections (you probably didn't know these)
- `Engine` --evaluates--> `Retention Policies`  [EXTRACTED]
  internal/retention/engine.go → docs/DESIGN.md
- `gRPC Server` --connects_clients_shown_on--> `Clients Page`  [INFERRED]
  images/001-activity.png → internal/webui/templates/fragments/clients.html
- `Engine` --streams_to--> `gRPC DataService`  [EXTRACTED]
  internal/backup/engine.go → docs/DESIGN.md
- `Engine` --uses--> `Manifest Exchange`  [EXTRACTED]
  internal/backup/engine.go → docs/DESIGN.md
- `Ongoing Backup` --uses--> `Engine`  [EXTRACTED]
  docs/DESIGN.md → internal/backup/engine.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **** — backup_engine, blake3, manifest_exchange, grpc_data_service [EXTRACTED]
- **** — file_watcher, debouncer, stability_gate, ongoing_backup [EXTRACTED]
- **** — aes_256_gcm, argon2id, blake3, content_addressable_storage [EXTRACTED]
- **** — grpc_command_service, grpc_data_service, mutual_tls [EXTRACTED]
- **** — scheduler, client_registry, grpc_command_service [EXTRACTED]
- **** — web_ui, htmx, alpine_js, server_sent_events [EXTRACTED]
- **** — docs_nat_mode, tunnel_hub, grpc_command_service, client_registry [EXTRACTED]
- **** — retention_engine, retention_policies, sqlite_database, content_addressable_storage [EXTRACTED]
- **** — cross_client_restore, grpc_data_service, aes_256_gcm, client_registry [EXTRACTED]
- **** — backup_engine, database_sync, sqlite_database, grpc_data_service [EXTRACTED]
- **** — feature_full_backup, feature_include_paths, feature_exclude_patterns [INFERRED]
- **** — feature_auto_backup, feature_file_watcher, feature_include_paths [INFERRED]
- **** — feature_remote_client_restore, feature_encryption, component_grpc_server [INFERRED]
- **** — component_client_registry, component_grpc_server, ui_clients_page [INFERRED]
- **** — feature_retention_policy, component_retention_engine, data_backup_record [INFERRED]
- **** — ui_dashboard_page, metric_cpu_load, metric_memory_usage, metric_total_files, metric_dedup_ratio, metric_storage_used, metric_active_clients, metric_network_speed [EXTRACTED]
- **** — concept_node_role, component_grpc_server, feature_file_watcher [INFERRED]
- **** — feature_deduplication, feature_full_backup, feature_ongoing_backup [INFERRED]

## Communities (163 total, 24 thin omitted)

### Community 0 - "Alpine.js Frontend Bundle"
Cohesion: 0.03
Nodes (93): A(), ae(), ai(), ao(), At(), be(), br(), Bt() (+85 more)

### Community 1 - "Crypto and Chunk Processing"
Cohesion: 0.07
Nodes (72): AESEncryptor, Encryptor, aesKeyUnwrap(), aesKeyWrap(), NewEncryptor(), T, TestProperty_CiphertextLength(), TestProperty_CiphertextNoncePrefix() (+64 more)

### Community 2 - "HTMX Frontend Bundle"
Cohesion: 0.11
Nodes (74): A(), ae(), ar(), at(), B(), be(), br(), c() (+66 more)

### Community 3 - "Deletion Command Logic"
Cohesion: 0.09
Nodes (46): Context, Repository, repoAdapter, DeleteResult, DeletionEngine, Engine, Filter, mockRepo (+38 more)

### Community 4 - "Client Registry"
Cohesion: 0.07
Nodes (42): Context, DB, Duration, RWMutex, Time, New(), parseDBTime(), DB (+34 more)

### Community 5 - "gRPC Client and Heartbeat"
Cohesion: 0.07
Nodes (41): ClientConfig, HeartbeatStateProvider, mockCommandForHeartbeat, TergumClient, Connect(), ConnectWithConfig(), DefaultClientConfig(), BackupLevel (+33 more)

### Community 6 - "Service Management CLI"
Cohesion: 0.08
Nodes (54): Command, newServiceCmd(), newServiceRestartCmd(), newServiceStartCmd(), newServiceStatusCmd(), newServiceStopCmd(), pidFilePath(), readPID() (+46 more)

### Community 7 - "Auth and Migration"
Cohesion: 0.10
Nodes (48): CopyFile(), DefaultArgon2idParams(), Duration, Handler, Request, ResponseWriter, RWMutex, Time (+40 more)

### Community 8 - "Database Repository"
Cohesion: 0.09
Nodes (12): DeleteFilter, JobUpdate, Repository, RestoreRecord, SQLiteRepository, backupColumns(), Context, DB (+4 more)

### Community 9 - "gRPC Mock Repository"
Cohesion: 0.07
Nodes (7): mockRepo, mockStore, Context, Duration, ReadCloser, Reader, Time

### Community 10 - "Observability and Metrics"
Cohesion: 0.07
Nodes (39): Counter, CounterVec, Gauge, Histogram, Writer, parseLevel(), RegisterLogListener(), SetupLogging() (+31 more)

### Community 11 - "Configuration Management"
Cohesion: 0.09
Nodes (41): BackupConfig, ClientConfig, Config, DatabaseConfig, EncryptionConfig, LoggingConfig, MetricsConfig, NodeConfig (+33 more)

### Community 12 - "Backup Scheduler"
Cohesion: 0.10
Nodes (28): Cron, EntryID, BackupLevel, Context, Duration, Mutex, missedBackupLevel(), New() (+20 more)

### Community 13 - "WebUI API Handlers"
Cohesion: 0.18
Nodes (8): escapeJS(), formatSize(), Context, Request, ResponseWriter, Time, Server, writeJSONError()

### Community 14 - "WebUI Navigation and Server"
Cohesion: 0.10
Nodes (35): itemVisibleForRole(), dirSizeQuick(), dirSizeWalk(), Config, parseTemplates(), WithBackupTrigger(), WithClientConnector(), WithClientRegistry() (+27 more)

### Community 15 - "Backup Trigger WebUI"
Cohesion: 0.09
Nodes (21): Engine, Mutex, Repository, NewLocalBackupTrigger(), Request, ResponseWriter, RWMutex, Time (+13 more)

### Community 16 - "Backup Entry Model"
Cohesion: 0.11
Nodes (5): Context, Duration, Time, BackupEntry, mockRepository

### Community 17 - "Connection Factory"
Cohesion: 0.14
Nodes (29): CheckClientEnabled(), Config, NewDataSource(), NewServerConnection(), containsStr(), containsSubstring(), T, TestNewDataSource_RoleClientMissingAddress() (+21 more)

### Community 18 - "gRPC Error Handling"
Cohesion: 0.15
Nodes (30): MapError(), T, TestProperty_AuthErrorMapping(), TestProperty_BackupFailedErrorMapping(), TestProperty_ConfigErrorMapping(), TestProperty_ConnectionErrorMapping(), TestProperty_GenericErrorMapping(), TestProperty_NilErrorMapping() (+22 more)

### Community 19 - "WebUI Asset Serving"
Cohesion: 0.12
Nodes (28): containsVersionPattern(), FS, Request, ResponseWriter, IsFingerprinted(), isFullHexMatch(), isHTMLContentType(), isKnownExtension() (+20 more)

### Community 20 - "gRPC Proto Service Registration"
Cohesion: 0.14
Nodes (23): NewCommandServer(), ServiceRegistrar, NewCommandServiceClient(), RegisterCommandServiceServer(), NewDataServiceClient(), NewRemoteServerConnection(), BackupLevel, BackupRequest (+15 more)

### Community 21 - "Database Repository Tests"
Cohesion: 0.19
Nodes (28): T, newTestRepo(), TestConcurrentReadsWAL(), TestCountHashReferences(), TestCreateAndUpdateJob(), TestDeleteEntries(), TestDeleteEntries_AllBackups(), TestDeleteEntries_ByBackupID() (+20 more)

### Community 22 - "Server Core"
Cohesion: 0.12
Nodes (20): clientsDirFromDB(), BackupLevel, BackupRequest, Config, Context, DB, Engine, Repository (+12 more)

### Community 23 - "TLS Certificate Management"
Cohesion: 0.20
Nodes (23): Join(), NewManager(), Certificate, T, loadCert(), TestGenerateCerts_CAProperties(), TestGenerateCerts_CertsSignedByCA(), TestGenerateCerts_ClientCertProperties() (+15 more)

### Community 24 - "WebUI Server Integration"
Cohesion: 0.23
Nodes (9): BackupTrigger, ClientRegistry, FilterNavItems(), Context, Request, ResponseWriter, Time, Server (+1 more)

### Community 25 - "Chunked Stream Processing"
Cohesion: 0.17
Nodes (22): errorReader, Reader, T, TestProperty_ChunkingRoundTrip(), Split(), StreamChunks(), T, TestJoinEmptySlice() (+14 more)

### Community 26 - "Backup Scanner"
Cohesion: 0.21
Nodes (22): Context, isHidden(), matchesExclude(), T, TestProperty_ScanExcludedFilesNeverAppear(), TestProperty_ScanFullIncludesAllMatchingFiles(), TestProperty_ScanOversizedFilesNeverAppear(), TestProperty_ScanResultsWithinIncludePaths() (+14 more)

### Community 27 - "Command Service Proto"
Cohesion: 0.20
Nodes (14): _CommandService_DeleteFromBackup_Handler(), _CommandService_GetRetention_Handler(), _CommandService_GetStatus_Handler(), _CommandService_ListBackups_Handler(), _CommandService_Ping_Handler(), _CommandService_RegisterClient_Handler(), _CommandService_StartWatcher_Handler(), _CommandService_StopBackup_Handler() (+6 more)

### Community 28 - "Remote Client Connector"
Cohesion: 0.19
Nodes (15): Config, Context, NewRemoteClientConnector(), T, newTestRegistryForConnector(), TestRemoteClientConnector_GetClientStatus_ClientNotFound(), TestRemoteClientConnector_ImplementsInterface(), TestRemoteClientConnector_StartWatcher_ClientNotFound() (+7 more)

### Community 29 - "Certificate Bootstrap"
Cohesion: 0.13
Nodes (17): BootstrapServer, BootstrapServerConfig, Context, NewBootstrapServer(), _BootstrapService_FetchClientCerts_Handler(), ClientConnInterface, Context, ServiceRegistrar (+9 more)

### Community 30 - "Restore Engine Tests"
Cohesion: 0.35
Nodes (23): HashBytes(), contains(), containsSubstr(), T, insertTestEntry(), insertTestJob(), setupTestEngine(), storeFileInCAS() (+15 more)

### Community 31 - "Data Stream Proto"
Cohesion: 0.12
Nodes (13): dataServiceUploadClient, FileChunk, FileChunk_Data, FileChunk_Header, FileChunk_Trailer, fileChunkJSON, FileHeader, FileTrailer (+5 more)

### Community 32 - "Restore Engine"
Cohesion: 0.19
Nodes (14): applyMetadata(), Context, Repository, NewRestoreEngine(), T, TestProperty_FileMetadataRoundTrip_Permissions(), TestProperty_FileMetadataRoundTrip_Symlinks(), TestProperty_FileMetadataRoundTrip_Timestamps() (+6 more)

### Community 33 - "Backup Command CLI"
Cohesion: 0.12
Nodes (18): Command, Config, loadMasterKey(), newBackupCmd(), parseMaxFileSize(), runBackup(), runRemoteClientBackup(), Command (+10 more)

### Community 34 - "File Watcher Core"
Cohesion: 0.15
Nodes (13): Event, Bool, CancelFunc, Context, Int64, Mutex, Repository, Time (+5 more)

### Community 35 - "Data Service gRPC"
Cohesion: 0.15
Nodes (14): _DataService_Download_Handler(), _DataService_SyncDatabase_Handler(), _DataService_Upload_Handler(), ServerStream, ServiceRegistrar, RegisterDataServiceServer(), DataService_DownloadServer, DataService_SyncDatabaseServer (+6 more)

### Community 36 - "Data Service Mocks"
Cohesion: 0.12
Nodes (12): _DataService_ExchangeManifest_Handler(), ClientStream, Context, ManifestDiff, UnaryServerInterceptor, Context, ManifestDiff, DataService_DownloadClient (+4 more)

### Community 37 - "Ongoing Backup Scheduler"
Cohesion: 0.17
Nodes (12): CancelFunc, Context, Duration, FileInfo, Mutex, Repository, WaitGroup, readFileRetry() (+4 more)

### Community 38 - "Alpine.js Internals A"
Cohesion: 0.16
Nodes (21): ar(), Ct(), F(), ft(), H(), Hn(), mr(), nt() (+13 more)

### Community 39 - "Backup Engine Core"
Cohesion: 0.21
Nodes (13): BackupEngine, BackupRequest, EngineConfig, LocalServerConnection, ServerConnection, BackupLevel, Bool, Context (+5 more)

### Community 40 - "TLS Certificate Generation"
Cohesion: 0.21
Nodes (14): CertPool, Int, generateCA(), generateClientCert(), generateServerCert(), Certificate, Config, loadCAPool() (+6 more)

### Community 41 - "gRPC Tunnel Hub"
Cohesion: 0.18
Nodes (9): clientTunnel, TunnelHub, BackupRequest, Context, Duration, Mutex, RWMutex, NewTunnelHub() (+1 more)

### Community 42 - "Command Server Operations"
Cohesion: 0.15
Nodes (13): BackupJobInfo, BackupLevel, BackupRequest, DeleteRequest, DeleteResponse, ListBackupsRequest, ListBackupsResponse, PushRestoreRequest (+5 more)

### Community 43 - "Domain Error Types"
Cohesion: 0.10
Nodes (7): AuthError, BackupFailedError, ConfigError, ConnectionError, ExitCoder, StoppedError, StorageError

### Community 44 - "Retention Engine Tests"
Cohesion: 0.37
Nodes (19): RetentionEngine, T, Time, insertTestEntry(), insertTestJob(), TestAddRemoveListPolicies(), TestEvaluate_DisabledPolicyIgnored(), TestEvaluate_DryRunDoesNotModifyDB() (+11 more)

### Community 45 - "CAS Dedup Tests"
Cohesion: 0.21
Nodes (17): T, TestProperty_DeduplicationAndRefCounting(), NewCAS(), Context, T, TestDeleteCleansUpEmptyParentDir(), TestDeleteNonExistentHashReturnsError(), TestDeleteRemovesFile() (+9 more)

### Community 46 - "Alpine.js Internals B"
Cohesion: 0.14
Nodes (20): an(), cn(), fi(), gt(), ii(), jt(), K(), li() (+12 more)

### Community 47 - "Alpine.js/HTMX Internals"
Cohesion: 0.16
Nodes (20): jn(), Jr(), rt(), set(), bt(), cr(), dr(), g() (+12 more)

### Community 48 - "Backup Manifest"
Cohesion: 0.19
Nodes (15): ManifestDiff, BuildManifest(), ComputeDiff(), T, TestProperty_ManifestDiffCorrectness(), T, TestBuildManifest_SkipsUnreadableFiles(), TestBuildManifest_WithTempFiles() (+7 more)

### Community 49 - "Proto Codec and Versioning"
Cohesion: 0.20
Nodes (11): init(), setToastTrigger(), T, TestSetErrorToast(), TestSetSuccessToast(), TestSetToastTrigger_AllTypes(), TestSetToastTrigger_Error(), TestSetToastTrigger_JSONFormat() (+3 more)

### Community 50 - "File Watcher Prop Tests"
Cohesion: 0.24
Nodes (17): NewFileWatcher(), T, TestProperty_DebounceCollapsesMultipleEvents(), TestProperty_ExcludeFilteringCorrectness(), TestProperty_ExcludeFilteringNeverPassesMatchedFiles(), TestProperty_StabilityGateDiscardsDeletedFiles(), Config, Repository (+9 more)

### Community 51 - "Restore Bug Prop Tests"
Cohesion: 0.20
Nodes (15): classifyQueryCurrent(), T, TestProperty_WindowsPathDetection(), TestProperty_WindowsPathNotExactName(), dedupEntriesCurrent(), T, TestProperty_DedupNilDEKNotSelected(), TestProperty_DedupSelectsValidDEK() (+7 more)

### Community 52 - "Command Tunnel Operations"
Cohesion: 0.15
Nodes (9): BackupRequest, commandServiceCommandTunnelClient, commandServiceCommandTunnelServer, PushRestoreInitiateRequest, StopRequest, StopResponse, TunnelCommand, TunnelRegistration (+1 more)

### Community 53 - "Data Server Core"
Cohesion: 0.16
Nodes (8): DataServer, DataServerConfig, Semaphore, clientIDFromContext(), Context, ManifestDiff, Repository, Context

### Community 54 - "Data Server Mocks"
Cohesion: 0.25
Nodes (15): mockDataServiceClient, NewDataServer(), Context, isEMFILEError(), SyncDatabaseToServer(), T, TestDataServer_SyncDatabase_EmptyStream(), TestDataServer_SyncDatabase_MissingClientID() (+7 more)

### Community 55 - "Ongoing Backup Tests"
Cohesion: 0.38
Nodes (14): NewOngoingBackup(), T, newMockRepo(), newMockServer(), newMockWatcher(), TestNewOngoingBackup_CustomBatchInterval(), TestNewOngoingBackup_DefaultBatchInterval(), TestOngoingBackup_EmptyBatchNotProcessed() (+6 more)

### Community 56 - "Root Command CLI"
Cohesion: 0.20
Nodes (14): Execute(), T, TestBackupCommandAcceptsLevel(), TestDeleteCommandAcceptsDryRun(), TestExecuteReturnsZeroForVersion(), TestExitCodeMapping(), TestGlobalFlagsExist(), TestRestoreCommandAcceptsFile() (+6 more)

### Community 57 - "Client Watcher Commands"
Cohesion: 0.17
Nodes (9): Context, ClientConnInterface, ClientStream, CommandService_CommandTunnelClient, CommandService_PushRestoreClient, CommandServiceClient, commandServicePushRestoreClient, WatcherRequest (+1 more)

### Community 58 - "Command Server API"
Cohesion: 0.21
Nodes (9): CommandServer, CommandServerConfig, DeletionEngine, RetentionEngine, BackupRequest, Context, Engine, Repository (+1 more)

### Community 59 - "Backup Engine Tests"
Cohesion: 0.36
Nodes (16): createTestFile(), findStoredFiles(), T, setupTestEngine(), TestLocalServerConnection_ExchangeManifest(), TestLocalServerConnection_SyncDatabase(), TestLocalServerConnection_UploadFile(), TestRunBackup_BackupLevels() (+8 more)

### Community 60 - "WebUI API Core"
Cohesion: 0.16
Nodes (10): GetLogHistory(), Repository, parseLogLine(), T, TestResolveDestination(), resolveDestination(), truncatePath(), writeRemoteRestoreJSON() (+2 more)

### Community 61 - "Watch Command CLI"
Cohesion: 0.31
Nodes (15): Command, Config, loadWatchMasterKey(), newWatchAddCmd(), newWatchCmd(), newWatchDisableCmd(), newWatchEnableCmd(), newWatchListCmd() (+7 more)

### Community 62 - "Job and Policy Persistence"
Cohesion: 0.17
Nodes (6): JobFilter, Time, BackupJob, BackupLevel, JobStatus, RetentionPolicy

### Community 63 - "Sync Database Mock Stream"
Cohesion: 0.17
Nodes (8): mockSyncDatabaseClientStream, mockSyncDatabaseServer, ClientStream, ServerStream, DatabaseChunk, dataServiceSyncDatabaseClient, dataServiceSyncDatabaseServer, SyncResponse

### Community 64 - "WebUI Template Handlers"
Cohesion: 0.23
Nodes (13): parseFragmentTemplates(), T, TestProperty_FragmentVsFullResponse(), Server, T, newTestServerForFragments(), TestParseFragmentTemplates(), TestParseFragmentTemplates_HasShellAndContent() (+5 more)

### Community 65 - "Scanner Platform Support"
Cohesion: 0.16
Nodes (12): ScannedFile, buildBackupEntry(), fillPlatformMetadata(), FileInfo, isReparsePoint(), Time, fillPlatformMetadata(), FileInfo (+4 more)

### Community 66 - "Remote Data Source"
Cohesion: 0.17
Nodes (8): RemoteDataSource, RemoteServerConnection, ClientConnInterface, Context, ManifestDiff, Context, NewRemoteDataSource(), DataServiceClient

### Community 67 - "Content-Addressable Storage"
Cohesion: 0.29
Nodes (7): Context, ReadCloser, Reader, validateHash(), CAS, RefCounter, Store

### Community 68 - "WebUI Sort Logic"
Cohesion: 0.24
Nodes (12): T, TestProperty_SortDirectionCycling(), TestProperty_SortDirectionDoubleToggleIdentity(), T, TestSortDirectionConstants(), TestToggleSortDirection_AscToDesc(), TestToggleSortDirection_DescToAsc(), TestToggleSortDirection_DoubleToggleReturnsOriginal() (+4 more)

### Community 69 - "Encryption Architecture"
Cohesion: 0.20
Nodes (14): AES-256-GCM Encryption, Argon2id Key Derivation, Engine, BLAKE3 Hashing, Cobra, Content-Addressable Storage, CLI Interface, Manifest Exchange (+6 more)

### Community 70 - "Client Command CLI"
Cohesion: 0.36
Nodes (13): formatTimeAgo(), Command, Time, newClientCmd(), newClientDisableCmd(), newClientEnableCmd(), newClientListCmd(), newClientStatusCmd() (+5 more)

### Community 71 - "Delete Command CLI"
Cohesion: 0.31
Nodes (11): Command, newDeleteCmd(), runDelete(), runDeleteActivity(), Command, newRetentionAddCmd(), newRetentionCmd(), newRetentionListCmd() (+3 more)

### Community 72 - "Server Infrastructure Components"
Cohesion: 0.21
Nodes (14): Client Registry, gRPC Server, Web UI Server, Client Status, Auto Backup, Exclude Patterns, File Watcher, Full Backup (+6 more)

### Community 73 - "Client Command Server"
Cohesion: 0.21
Nodes (10): ClientCommandServer, ClientCommandServerConfig, WatcherFactory, CancelFunc, Config, Engine, Mutex, Repository (+2 more)

### Community 74 - "Retention Dry-Run Tests"
Cohesion: 0.34
Nodes (10): captureCASSnapshot(), captureDBSnapshot(), dryRunSetup(), genSmallHash(), RetentionEngine, T, TestProperty_DeletionDryRunIdempotence(), TestProperty_RetentionDryRunIdempotence() (+2 more)

### Community 75 - "Retention Engine Core"
Cohesion: 0.25
Nodes (8): findMatchingPolicy(), Context, Repository, Time, New(), shouldExpire(), RetentionEngine, RetentionResult

### Community 76 - "Mock Watcher Tests"
Cohesion: 0.17
Nodes (5): mockTestWatcher, Context, T, TestClientCommandServer_WatcherControl(), WatcherStatus

### Community 77 - "Dashboard Cards API"
Cohesion: 0.31
Nodes (6): formatSpeed(), getStatusColor(), getStatusIcon(), Request, ResponseWriter, Server

### Community 78 - "WebUI Paths Handler"
Cohesion: 0.49
Nodes (4): Context, Request, ResponseWriter, Server

### Community 79 - "Watcher Controller WebUI"
Cohesion: 0.21
Nodes (7): CancelFunc, Context, Mutex, Repository, NewLocalWatcherController(), LocalWatcherConfig, LocalWatcherController

### Community 80 - "Frontend Tech Stack"
Cohesion: 0.18
Nodes (12): Alpine.js, Go Embed, HTMX, Server-Sent Events (SSE), Structured Logging, Tailwind CSS, Activity Page, Connection Status (+4 more)

### Community 81 - "Paths Command CLI"
Cohesion: 0.52
Nodes (11): Command, Repository, newPathsAddCmd(), newPathsCmd(), newPathsExcludeCmd(), newPathsListCmd(), newPathsRemoveCmd(), newPathsScanCmd() (+3 more)

### Community 82 - "Dashboard Metrics Display"
Cohesion: 0.17
Nodes (12): Activity Entry, Deduplication, Active Clients, CPU Load, Dedup Ratio, Memory Usage, Network Speed, Storage Used (+4 more)

### Community 83 - "Push Restore and Tunnel"
Cohesion: 0.18
Nodes (6): _CommandService_CommandTunnel_Handler(), _CommandService_PushRestore_Handler(), ServerStream, CommandService_PushRestoreServer, commandServicePushRestoreServer, PushRestoreResponse

### Community 84 - "gRPC Client Prop Tests"
Cohesion: 0.45
Nodes (11): computeBackoffSequence(), genClientConfig(), ClientConfig, Duration, T, TestProperty_BackoffSequenceFirstElement(), TestProperty_BackoffSequenceLength(), TestProperty_BackoffSequenceMonotonicallyIncreasing() (+3 more)

### Community 85 - "Integration Tests"
Cohesion: 0.39
Nodes (10): countPhysicalFiles(), T, setupIntegrationEnv(), TestIntegration_BackupAndRestore(), TestIntegration_BackupStopGraceful(), TestIntegration_BackupWithEncryptionAndRestore(), TestIntegration_DedupAcrossBackups(), TestIntegration_MutualTLSHandshake() (+2 more)

### Community 86 - "WebUI Metrics Handler"
Cohesion: 0.24
Nodes (9): T, TestProperty_StorageThresholdColorScheme(), StorageColorScheme(), T, TestDiskUsagePercent_EmptyPath(), TestDiskUsagePercent_NonexistentPath(), TestDiskUsagePercent_ValidPath(), TestStorageColorScheme_Boundaries() (+1 more)

### Community 87 - "WebUI Toast Notifications"
Cohesion: 0.23
Nodes (7): ResponseWriter, T, TestProperty_ToastMessageTruncation(), setErrorToast(), setToastAndEvent(), TruncateToastMessage(), toastPayload

### Community 88 - "WebUI Toast and Watchers"
Cohesion: 0.42
Nodes (4): setSuccessToast(), Request, ResponseWriter, Server

### Community 89 - "NAT and Cross-Client Features"
Cohesion: 0.22
Nodes (11): Certificate Bootstrap, Client Registry, Cross-Client Restore, Database Sync, NAT Mode, gRPC CommandService, gRPC DataService, gRPC + Protobuf (+3 more)

### Community 90 - "Restore Command CLI"
Cohesion: 0.31
Nodes (10): Command, Config, Context, Repository, loadRestoreMasterKey(), lookupClientAddress(), newRestoreCmd(), resolveDestination() (+2 more)

### Community 91 - "BLAKE3 Hash Functions"
Cohesion: 0.35
Nodes (9): HashFile(), T, TestHashBytes_Deterministic(), TestHashBytes_DifferentInputs(), TestHashBytes_Empty(), TestHashFile_EmptyFile(), TestHashFile_LargeFile(), TestHashFile_NonExistent() (+1 more)

### Community 92 - "Server Tests"
Cohesion: 0.36
Nodes (10): New(), T, TestClientsDirFromDB(), TestNew_DefaultState(), TestNew_ReturnsServer(), TestNoopBackupEngine(), TestRunRetentionLoop_StopsOnCancel(), TestStop_Idempotent() (+2 more)

### Community 93 - "Navigation Tests"
Cohesion: 0.42
Nodes (10): assertHasItem(), assertNoItem(), T, TestFilterNavItems_ClientRole(), TestFilterNavItems_EmptyRole(), TestFilterNavItems_HybridRole(), TestFilterNavItems_ItemsHaveLabelsAndIcons(), TestFilterNavItems_PreservesOrder() (+2 more)

### Community 94 - "Database Write Queue"
Cohesion: 0.29
Nodes (6): WriteQueue, writeRequest, CancelFunc, Context, WaitGroup, NewWriteQueue()

### Community 95 - "Tunnel Client"
Cohesion: 0.50
Nodes (8): TunnelClientConfig, Context, Duration, handleTunnelCommand(), runTunnelSession(), StartTunnel(), Logger(), CommandServiceServer

### Community 96 - "Retention Prop Tests"
Cohesion: 0.44
Nodes (8): genFilePaths(), genRetentionPolicies(), genVersionDates(), RetentionEngine, T, Time, propSetup(), TestProperty_RetentionEvaluationSafetyAndCorrectness()

### Community 97 - "Alpine.js Effects"
Cohesion: 0.28
Nodes (9): effect(), Je(), Ve(), x(), Xn(), Yn(), nt(), tt() (+1 more)

### Community 98 - "Config Settings WebUI"
Cohesion: 0.31
Nodes (7): applyConfigValue(), Config, Request, ResponseWriter, Server, parseBool(), syncInMemoryConfig()

### Community 99 - "Path Validator"
Cohesion: 0.28
Nodes (6): T, TestProperty_NonWhitespacePathsAccepted(), TestProperty_WhitespaceOnlyPathsRejected(), T, TestValidatePath(), ValidatePath()

### Community 100 - "Admin Command CLI"
Cohesion: 0.32
Nodes (7): deriveKeyFromEnv(), Command, Config, newAdminCmd(), runAdmin(), Repository, WithRepository()

### Community 101 - "Status Command CLI"
Cohesion: 0.43
Nodes (7): countRegisteredClients(), dirSize(), formatBytes(), getCACertFingerprint(), Command, newStatusCmd(), runStatus()

### Community 102 - "Config Settings Display"
Cohesion: 0.25
Nodes (8): Node Role, Encryption Settings, Network Settings, Node Settings, Storage Settings, Setup Wizard, TOML Configuration, Config Page

### Community 103 - "Node Command CLI"
Cohesion: 0.62
Nodes (6): Command, newNodeCmd(), newNodeHostnameCmd(), newNodeRoleCmd(), newNodeShowCmd(), DefaultConfigPath()

### Community 104 - "Retention Concepts"
Cohesion: 0.29
Nodes (7): Retention Engine, Backup Level, Backup Status, Backup Record, Retention Policy, Retention Policies, Retention Page

### Community 105 - "Client Backup Trigger"
Cohesion: 0.33
Nodes (4): ParseMaxFileSize(), BackupRequest, BackupRequest, BackupResponse

### Community 106 - "Backup Stop API Tests"
Cohesion: 0.29
Nodes (3): T, TestHandleAPIBackupStop(), mockBackupTrigger

### Community 107 - "Staleness Detection Tests"
Cohesion: 0.48
Nodes (6): T, TestPollingStalenessState_BecomeStaleAt3Failures(), TestPollingStalenessState_InitialState(), TestPollingStalenessState_StaleRemainsAfterMoreFailures(), TestPollingStalenessState_SuccessResets(), TestPollingStalenessState_SuccessResetsBeforeStale()

### Community 108 - "Blocking Server Connection"
Cohesion: 0.47
Nodes (3): blockingServerConnection, Context, ManifestDiff

### Community 109 - "Build Scripts Shell"
Cohesion: 0.80
Nodes (5): build_all(), build.sh script, build_darwin(), build_linux(), build_windows()

### Community 110 - "Service Unix Detach"
Cohesion: 0.40
Nodes (5): detachProcess(), Cmd, Process, isProcessRunning(), terminateProcess()

### Community 111 - "Service Windows Detach"
Cohesion: 0.33
Nodes (4): detachProcess(), Cmd, Process, terminateProcess()

### Community 112 - "Remote Restore Features"
Cohesion: 0.33
Nodes (6): Browse Backup Files, Encryption, Remote Client Restore, Restore by Query, Restore Entire Backup, Restore Page

### Community 113 - "Integration Test Helpers"
Cohesion: 0.47
Nodes (3): Context, ManifestDiff, stoppingServerConnection

### Community 116 - "System Stats Linux"
Cohesion: 0.60
Nodes (5): getCPULoad(), getMemoryStats(), getSystemStats(), parseMemInfoValue(), readCPUStat()

### Community 117 - "System Stats Windows"
Cohesion: 0.53
Nodes (5): getCPULoad(), getMemoryStats(), getSystemStats(), getSystemTimes(), MEMORYSTATUSEX

### Community 118 - "Backup Result Mocks"
Cohesion: 0.40
Nodes (3): BackupResult, mockBackupEngine, BackupRequest

### Community 119 - "List Command CLI"
Cohesion: 0.60
Nodes (4): formatSpeed(), Command, newListCmd(), runList()

### Community 120 - "File Watcher Architecture"
Cohesion: 0.40
Nodes (5): Debouncer, File Watcher, fsnotify, Stability Gate, Watchers Page

### Community 122 - "CAS BLAKE3 Prop Tests"
Cohesion: 0.60
Nodes (4): T, TestProperty_BLAKE3Determinism(), TestProperty_CASExistsAfterPut(), TestProperty_CASRoundTrip()

### Community 123 - "Node Settings WebUI"
Cohesion: 0.60
Nodes (3): Request, ResponseWriter, Server

### Community 124 - "System Stats macOS"
Cohesion: 0.70
Nodes (4): getCPULoad(), getMemoryStats(), getSystemStats(), readCPUTicks()

### Community 126 - "Cleanup Command CLI"
Cohesion: 0.67
Nodes (3): Command, newCleanupCmd(), runCleanup()

### Community 127 - "Stop Command CLI"
Cohesion: 0.67
Nodes (3): Command, newStopCmd(), runStop()

### Community 129 - "Env File Loader"
Cohesion: 0.83
Nodes (3): Load(), parseLine(), unquote()

### Community 130 - "WebUI Handlers Helpers"
Cohesion: 0.50
Nodes (3): Request, ResponseWriter, Server

### Community 134 - "UI Shell Components"
Cohesion: 0.67
Nodes (3): Modal Partial, Shell Template, Toast Partial

## Knowledge Gaps
- **80 isolated node(s):** `bump_version.sh script`, `github.com/gcclinux/tergum`, `ClientConfig`, `Repository`, `Engine` (+75 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **24 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Join()` connect `TLS Certificate Management` to `Crypto and Chunk Processing`, `Service Management CLI`, `Database Repository`, `Configuration Management`, `WebUI API Handlers`, `Connection Factory`, `gRPC Proto Service Registration`, `Server Core`, `Chunked Stream Processing`, `Backup Scanner`, `Certificate Bootstrap`, `Restore Engine Tests`, `Restore Engine`, `Backup Command CLI`, `Data Service gRPC`, `Backup Engine Core`, `TLS Certificate Generation`, `CAS Dedup Tests`, `Backup Manifest`, `File Watcher Prop Tests`, `Restore Bug Prop Tests`, `Data Server Mocks`, `Ongoing Backup Tests`, `Backup Engine Tests`, `WebUI API Core`, `Watch Command CLI`, `Job and Policy Persistence`, `Content-Addressable Storage`, `Client Command CLI`, `WebUI Paths Handler`, `Paths Command CLI`, `Push Restore and Tunnel`, `Integration Tests`, `Restore Command CLI`, `BLAKE3 Hash Functions`, `Server Tests`, `Admin Command CLI`, `Status Command CLI`, `Node Command CLI`, `Stop Command CLI`?**
  _High betweenness centrality (0.288) - this node is a cross-community bridge._
- **Why does `NewServer()` connect `Auth and Migration` to `WebUI Template Handlers`, `Admin Command CLI`, `Observability and Metrics`, `Configuration Management`, `WebUI Navigation and Server`, `Backup Trigger WebUI`, `WebUI Asset Serving`, `gRPC Proto Service Registration`, `Server Core`, `WebUI Server Integration`, `WebUI API Core`?**
  _High betweenness centrality (0.085) - this node is a cross-community bridge._
- **Why does `NewRepository()` connect `Delete Command CLI` to `Service Management CLI`, `Database Repository`, `WebUI API Handlers`, `gRPC Proto Service Registration`, `Database Repository Tests`, `Server Core`, `Restore Engine Tests`, `Restore Engine`, `Backup Command CLI`, `Retention Engine Tests`, `CAS Dedup Tests`, `File Watcher Prop Tests`, `Backup Engine Tests`, `Watch Command CLI`, `Retention Dry-Run Tests`, `Paths Command CLI`, `Integration Tests`, `Restore Command CLI`, `Retention Prop Tests`, `Admin Command CLI`, `Status Command CLI`, `List Command CLI`, `Cleanup Command CLI`, `Stop Command CLI`?**
  _High betweenness centrality (0.077) - this node is a cross-community bridge._
- **Are the 171 inferred relationships involving `Join()` (e.g. with `.RunBackup()` and `.ExchangeManifest()`) actually correct?**
  _`Join()` has 171 INFERRED edges - model-reasoned connections that need verification._
- **Are the 42 inferred relationships involving `NewRepository()` (e.g. with `runAdmin()` and `runBackup()`) actually correct?**
  _`NewRepository()` has 42 INFERRED edges - model-reasoned connections that need verification._
- **Are the 42 inferred relationships involving `NewEncryptor()` (e.g. with `deriveKeyFromEnv()` and `loadMasterKey()`) actually correct?**
  _`NewEncryptor()` has 42 INFERRED edges - model-reasoned connections that need verification._
- **What connects `bump_version.sh script`, `github.com/gcclinux/tergum`, `ClientConfig` to the rest of the system?**
  _80 weakly-connected nodes found - possible documentation gaps or missing edges._