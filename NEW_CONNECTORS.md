So, I want to build a new connector
It would be for the OpenSearch tool.
Since HNB is a tool focused in SQL instead of other languages, the idea is to continue using only SQL in notebooks.
As such, by what I've seen, OpenSearch has a native thing to convert SQL to their own language.
As far as my understanding goes, it would need to query one endpoint to get the translation, then make another web request to get the data itself.

The path of the plugin that gives the translation is `/_plugins/_sql/_explain` (Or maybe `/_plugins/_sql` ?)
To be able to develop and test this, the @docker-compose.dev.yml needs to also spin-up an opensearch, and seed it with data/index, since it seems this plugin for SQL conversion is very strict and only converts things where there is a match for both the columns and the index you are searching for.

It seems the project is this one: https://github.com/opensearch-project/sql
You can look at it for examples on how to use, how to connect and interact with this sql interface on opensearch, but ultimately, you should follow the conventions and interfaces for connectors in HNB.

If there are options and possibilities on how to configure/query the opensearch, if it makes sense, make these available as config values that can be set when registering the connector in HNB. If it require expanding the connector contracts in HNB, do so.

Make any necessary research and documentation reading about every part of its features and limitations before thinking about how. For example, IIRC, elastic/opensearch have a hard limit of maximum 10k rows for results. How does it impact queries? What do we need to have in mind querying such database?

With all of that, use the brainstorming and design-writing skill to create a draft on how this would be implemented.
