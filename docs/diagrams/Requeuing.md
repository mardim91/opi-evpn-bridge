# Requeue failed VRF Task

This sequence diagram explains the process where a module (e.g. Vendor Linux module) fails to apply some configuration to the underline system and therefore the Task needs to be requeued by the Task Manager component. More specifically when an intent gets created in the database (e.g. VRF intent) a corresponding Task also gets created by the Task Manager component and it is pushed in to a Queue of Tasks. When the Task gets dequeued the Task Manager component will send notifications regarding the realization of the Task to the corresponding modules (e.g. General Linux Module, Vendor Linux Module, Frr Module, Vendor Module) sequencially. If a Module fails to realize the Task then the Task gets requeued after a Timer (exponential backoff) gets expired and the failed Module receives a new notification to realize the Task again. If this time the formely failed Module succeeds then the next Module gets notified until the Task is fully realized. In this example here the Module that it fails to realize the Task is the Venodr Linux Module.



```mermaid
sequenceDiagram
    actor User as User
    participant GrpcServer as GrpcServer
    participant InfraDB as InfraDB
    participant TaskManager as TaskManager
    participant DB as DB
    participant TaskQueue as TaskQueue
    participant FrrModule as FrrModule
    participant Frr as Frr
    participant GenLinuxModule as GenLinuxModule
    participant VendorLinuxModule as VendorLinuxModule
    participant VendorModule as VendorModule
    participant XpuFastpath as XpuFastpath
    participant XpuLinuxSlowpath as XpuLinuxSlowpath

    User ->> GrpcServer: Create protobuf VRF

    GrpcServer ->> GrpcServer: Convert to infraDB model

    GrpcServer ->> InfraDB: Create infradb VRF

rect rgb(255, 229, 204)
    note right of User: Under global lock

    InfraDB ->> DB: Store VRF intent
    InfraDB ->> TaskManager: Create a task to realize the stored VRF intent
    TaskManager ->> TaskQueue: Push VRF Task into the Task Queue
    GrpcServer -->> User: VRF has been created
end

%% General Linux Module section
rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskQueue ->> TaskManager: Dequeue VRF Task for processing
    TaskManager ->> GenLinuxModule: Notify GenLinux module
    TaskManager ->> TaskManager: Wait until General Linux module update the Status of the VRF object in the DB
end


rect rgb(192, 192, 192)
    note right of InfraDB: GenLinux module thread
    GenLinuxModule ->> InfraDB: Get VRF infradb object
    InfraDB -->> GenLinuxModule: VRF infradb object
    note right of GenLinuxModule: Succesfull Execution
    GenLinuxModule ->> XpuLinuxSlowpath: ApplyLinuxConf() 

    rect rgb(255, 229, 204)
        note right of InfraDB: Under global lock
        GenLinuxModule ->> InfraDB: Update VRF infradb object module status to Success
        InfraDB ->> TaskManager: VRF infradb object status has been updated from General Linux module perspective
        InfraDB -->> GenLinuxModule: Status has been updated
    end
end

%% Vendor Linux Module section
rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskManager ->> TaskManager: wake up and move on to the Vendor Linux module
    TaskManager ->> VendorLinuxModule: Notify VendorLinux module
    TaskManager ->> TaskManager: Wait until Vendor Linux module update the Status of the VRF object in the DB
end


rect rgb(192, 192, 192)
    note right of InfraDB: VendorLinux module thread
    VendorLinuxModule ->> InfraDB: Get VRF infradb object
    InfraDB -->> VendorLinuxModule: VRF infradb object
    note right of VendorLinuxModule: Failed Execution
    VendorLinuxModule ->> XpuLinuxSlowpath: ApplyLinuxConf()
    VendorLinuxModule ->> VendorLinuxModule: Calculate an exponential back off Timer for requeuing

    rect rgb(255, 229, 204)
        note right of InfraDB: Under global lock
        VendorLinuxModule ->> InfraDB: Update VRF infradb object module status to Error and pass the Timer to Status struct
        InfraDB ->> TaskManager: VRF infradb object status has been updated from Vendor Linux module perspective
        InfraDB -->> VendorLinuxModule: Status has been updated
    end
end

rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskManager ->> TaskManager: (Separate go routine) Wait for the reported Timer to expire
    TaskManager ->> TaskQueue: Requeue failed VRF Task
    TaskQueue ->> TaskManager: Dequeue VRF failed Task for processing again
    TaskManager ->> VendorLinuxModule: Notify VendorLinux module
    TaskManager ->> TaskManager: Wait until Vendor Linux module update the Status of the VRF object in the DB
end

rect rgb(192, 192, 192)
    note right of InfraDB: VendorLinux module thread
    VendorLinuxModule ->> InfraDB: Get VRF infradb object
    InfraDB -->> VendorLinuxModule: VRF infradb object
    note right of VendorLinuxModule: Successful Execution
    VendorLinuxModule ->> XpuLinuxSlowpath: ApplyLinuxConf()

    rect rgb(255, 229, 204)
        note right of InfraDB: Under global lock
        VendorLinuxModule ->> InfraDB: Update VRF infradb object module status to Success
        InfraDB ->> TaskManager: VRF infradb object status has been updated from Vendor Linux module perspective
        InfraDB -->> VendorLinuxModule: Status has been updated
    end
end

%% FRR Module section
rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskManager ->> TaskManager: wake up and move on to the FRR module
    TaskManager ->> FrrModule: Notify Frr module
    TaskManager ->> TaskManager: Wait until FRR module update the Status of the VRF object in the DB
end


rect rgb(192, 192, 192)
    note right of InfraDB: Frr module thread
    FrrModule ->> InfraDB: Get VRF infradb object
    InfraDB -->> FrrModule: VRF infradb object
    note right of FrrModule: Successful Execution
    FrrModule ->> Frr: ApplyFrrConf()

    rect rgb(255, 229, 204)
        note right of InfraDB: Under global lock
        FrrModule ->> InfraDB: Update VRF infradb object module status to Success
        InfraDB ->> TaskManager: VRF infradb object status has been updated from FRR module perspective
        InfraDB -->> FrrModule: Status has been updated
    end
end

%% Vendor Module section
rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskManager ->> TaskManager: wake up and move on to the Vendor module
    TaskManager ->> VendorModule: Notify Vendor module
    TaskManager ->> TaskManager: Wait until Vendor module update the Status of the VRF object in the DB
end


rect rgb(192, 192, 192)
    note right of InfraDB: Vendor module thread
    VendorModule ->> InfraDB: Get VRF infradb object
    InfraDB -->> VendorModule: VRF infradb object
    note right of VendorModule: Successful Execution
    VendorModule ->> XpuFastpath: ApplyFastPathConf()

    rect rgb(255, 229, 204)
        note right of InfraDB: Under global lock
        VendorModule ->> InfraDB: Update VRF infradb object module status to success
        InfraDB ->> InfraDB: Update VRF object overall operational status to UP
        InfraDB ->> TaskManager: VRF infradb object status has been updated from Vendor module perspective
        InfraDB -->> VendorModule: Status has been updated
    end
end

%% Module Section ends

rect rgb(204, 255, 255)
    note right of TaskManager: Task Manager Thread
    TaskManager ->> TaskManager: Drop VRF realization Task. The VRF intent has been finally realized succesfully
end

    
```