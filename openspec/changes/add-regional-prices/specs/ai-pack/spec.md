## ADDED Requirements

### Requirement: Each call is priced at its region's price
The AI pack SHALL price a call at its model's price in the region the call was made from, from the release and shared tables merged tighten-only for that region. A model with no price in that region SHALL be priced at its highest price in any region. A config override SHALL apply in every region. A call through a global inference profile SHALL be priced at the regional price.

#### Scenario: A call in London
- **WHEN** an account calls a model in eu-west-2, where it costs $0.0062 per 1,000 input tokens, against $0.004 in us-east-1
- **THEN** the call's input is priced at $0.0062

#### Scenario: No price in the call's region
- **WHEN** an account calls a model in ap-southeast-7, and the tables price it only in us-east-1 at $0.004 and in eu-west-2 at $0.0062
- **THEN** the call's input is priced at $0.0062

#### Scenario: An override
- **WHEN** config overrides a model's price, and an account calls it in sa-east-1
- **THEN** the call is priced at the override
