# FRR Error Recovery (Based on Create VRF example)

```mermaid
sequenceDiagram
    actor User as User
    participant GrpcServer as GrpcServer
    participant InfraDB as InfraDB
    participant TaskManager as TaskManager
    participant FrrModule as FrrModule
    participant Frr as Frr

    User ->> GrpcServer: Create VRF request
    GrpcServer ->> InfraDB: Create VRF object intent
    InfraDB ->> TaskManager: Create VRF Task
    InfraDB -->> GrpcServer: Request has been processed
    GrpcServer -->> User: Request has been processed
    TaskManager ->> FrrModule: Send Notification (1st Try)
    FrrModule ->> Frr: Apply Frr config
    Frr -->> FrrModule: Error
    FrrModule -->> InfraDB: Update status of VRF for FRR to Error
    InfraDB ->> TaskManager: VRF Task Status: Error
    TaskManager ->> TaskManager: Requeue
    TaskManager ->> FrrModule: Send Notification (Nth Try)
    FrrModule ->> Frr: Apply Frr config
    Frr -->> FrrModule: Error
    FrrModule ->> FrrModule: The retries are too many, request "replay"
    FrrModule -->> InfraDB: Update status of VRF for FRR to Error and replay=True
    InfraDB ->> TaskManager: VRF Task Status: Error, dropTask=True and replay=True
    TaskManager ->> TaskManager: Wait until the object intents created in DB again
    InfraDB ->> InfraDB: Calls replay function
    InfraDB ->> FrrModule: Inside Replay Function: Send notification to the FrrModule to initiate pre-replay steps
    FrrModule ->> FrrModule: replace current FRR config with "basic-FRR-Config" and restart FRR
    FrrModule -->> InfraDB: Inside Replay Function: pre-replay steps has been completed
    InfraDB ->> InfraDB: Inside Replay Function: Fetch all the objects from DB (VRF, SVI) that are related to the FRR module that requested the "replay"
    InfraDB ->> InfraDB: Inside Replay Function: In the fetched objects put the status in Pending for the affected FrrModule and of the modules that are not in Success state
    InfraDB ->> InfraDB: Inside Replay Function: Store the objects in the DB again
    InfraDB ->> TaskManager: Inside Replay Function: Send Signal that the object intents has been created in DB
    TaskManager ->> TaskManager: Start processing of the tasks again normally
    InfraDB ->> InfraDB: Inside Replay Function: Create tasks for all the objects that needs to be replayed
    TaskManager ->> FrrModule: Notify the FrrModule (and the rest of the affected modules) to realize the created objects "intents".
    FrrModule ->> Frr: Apply Frr Config
    InfraDB ->> InfraDB: replay has been finished.